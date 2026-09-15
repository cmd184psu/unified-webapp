package timetracker

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler wires HTTP routes to the timetracker store.
type Handler struct {
	store *Store
}

// NewHandler returns a Handler.
func NewHandler(s *Store) *Handler {
	return &Handler{store: s}
}

// Register mounts all timetracker API routes on mux.
//
// Every exact path also gets a bare (method-less) fallback registration so a
// wrong-method request answers with the same JSON envelope the rest of the
// API uses, instead of net/http's default plain-text 405.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /data", h.handleGetData)
	mux.HandleFunc("/data", methodNotAllowed)

	mux.HandleFunc("POST /update", h.handleUpdate)
	mux.HandleFunc("/update", methodNotAllowed)

	mux.HandleFunc("POST /delete", h.handleDelete)
	mux.HandleFunc("/delete", methodNotAllowed)

	mux.HandleFunc("POST /create-customer", h.handleCreateCustomer)
	mux.HandleFunc("/create-customer", methodNotAllowed)

	mux.HandleFunc("GET /export-csv", h.handleExportCSV)
	mux.HandleFunc("/export-csv", methodNotAllowed)

	mux.HandleFunc("POST /import-csv", h.handleImportCSV)
	mux.HandleFunc("/import-csv", methodNotAllowed)
}

// methodNotAllowed answers a request whose method wasn't claimed by any of
// the method-tagged patterns registered for the same path.
func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// GET /data
func (h *Handler) handleGetData(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, h.store.Snapshot())
}

// updateRequest is the POST /update body shape.
type updateRequest struct {
	Index int             `json:"index"`
	Field string          `json:"field"`
	Value json.RawMessage `json:"value"`
}

// POST /update
//
// Dispatch order matches FRD §3.2 literally (author check first):
//  1. Index == -1 (any field): the author update path.
//  2. Field == "newCustomer" (FR-F1): add a customer; the request's Index
//     is otherwise ignored. A blank customerName is rejected so the list
//     never gains unnamed rows.
//  3. Otherwise: a single-field update on the customer at Index, where
//     Index addresses the same sorted order GET /data serves (the store's
//     canonical order).
func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	if req.Index == -1 {
		var value string
		if err := json.Unmarshal(req.Value, &value); err != nil {
			response.WriteError(w, http.StatusBadRequest, "value must be a string")
			return
		}
		data, err := h.store.SetAuthor(value)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, data)
		return
	}

	if req.Field == "newCustomer" {
		var c Customer
		if err := json.Unmarshal(req.Value, &c); err != nil {
			response.WriteError(w, http.StatusBadRequest, "value must be a customer object")
			return
		}
		if strings.TrimSpace(c.CustomerName) == "" {
			response.WriteError(w, http.StatusBadRequest, "customerName must not be empty")
			return
		}
		data, err := h.store.AppendCustomer(c)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, data)
		return
	}

	var value string
	if err := json.Unmarshal(req.Value, &value); err != nil {
		response.WriteError(w, http.StatusBadRequest, "value must be a string")
		return
	}
	data, err := h.store.UpdateField(req.Index, req.Field, value)
	if err != nil {
		if errors.Is(err, ErrInvalidIndex) || errors.Is(err, ErrInvalidField) {
			response.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, data)
}

// POST /delete
func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	data, err := h.store.DeleteCustomer(req.Index)
	if err != nil {
		if errors.Is(err, ErrInvalidIndex) {
			response.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, data)
}

// POST /create-customer
func (h *Handler) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	data, err := h.store.AppendCustomer(Customer{CustomerName: "New Customer"})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, data)
}

// csvColumns is the export/import column order: the reference order minus
// the removed legacy remote-access field (FR-F6) and InsightUrl, with
// SfdcUrl/CumulusBucket renamed to CmsUrl/SupportBucket. Import still
// accepts the legacy 8- and 9-column widths positionally.
var csvColumns = []string{
	"CustomerName", "SlackChannel", "SlackChannelId",
	"WorkLoadType", "CmsUrl", "SupportBucket", "Jira",
}

// GET /export-csv
func (h *Handler) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	data := h.store.Snapshot()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=customers.csv")

	writer := csv.NewWriter(w)
	_ = writer.Write(csvColumns)
	for _, c := range data.Customers {
		_ = writer.Write([]string{
			c.CustomerName, c.SlackChannel, c.SlackChannelId,
			c.WorkLoadType, c.CmsUrl, c.SupportBucket, c.Jira,
		})
	}
	writer.Flush()
}

// POST /import-csv
//
// Reads every row into memory before mutating the store (FR-F4): each data
// row must have 7 fields (the current column order, mapped positionally),
// 8 fields (legacy pshelper export — index 3, InsightUrl, is dropped and
// SfdcUrl/CumulusBucket land in CmsUrl/SupportBucket), or 9 fields (older
// legacy export — index 6, the removed remote-access field, is dropped
// too). Any other width, a failed header read, or a header-only file with
// zero data rows is a 400 with no store mutation.
func (h *Handler) handleImportCSV(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to parse uploaded file")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read CSV")
		return
	}
	if len(rows) < 1 {
		response.WriteError(w, http.StatusBadRequest, "CSV file is empty")
		return
	}
	dataRows := rows[1:]
	if len(dataRows) == 0 {
		response.WriteError(w, http.StatusBadRequest, "CSV file has no data rows")
		return
	}

	customers := make([]Customer, 0, len(dataRows))
	for _, row := range dataRows {
		var c Customer
		switch len(row) {
		case 7:
			c = Customer{
				CustomerName:   row[0],
				SlackChannel:   row[1],
				SlackChannelId: row[2],
				WorkLoadType:   row[3],
				CmsUrl:         row[4],
				SupportBucket:  row[5],
				Jira:           row[6],
			}
		case 8:
			c = Customer{
				CustomerName:   row[0],
				SlackChannel:   row[1],
				SlackChannelId: row[2],
				WorkLoadType:   row[4],
				CmsUrl:         row[5],
				SupportBucket:  row[6],
				Jira:           row[7],
			}
		case 9:
			c = Customer{
				CustomerName:   row[0],
				SlackChannel:   row[1],
				SlackChannelId: row[2],
				WorkLoadType:   row[4],
				CmsUrl:         row[5],
				SupportBucket:  row[7],
				Jira:           row[8],
			}
		default:
			response.WriteError(w, http.StatusBadRequest, "malformed CSV row")
			return
		}
		customers = append(customers, c)
	}

	data, err := h.store.ReplaceCustomers(customers)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, data)
}
