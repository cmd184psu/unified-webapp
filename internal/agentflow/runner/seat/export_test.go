package seat

import "cmd184psu/unified-webapp/internal/agentflow/runner/seatkit"

func finalFor(role, report string) (string, error) {
	r, err := seatkit.ParseReport([]byte(report))
	if err != nil {
		return "", err
	}
	return seatkit.Final(role, r)
}
