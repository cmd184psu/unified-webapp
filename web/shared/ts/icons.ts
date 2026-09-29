// icons.ts — the shared library's inline SVG icons, assembled node by node
// (never innerHTML), sized to the surrounding text (1em). Internal: modules
// get icons through the components that use them, not from the barrel.

const SVG_NS = "http://www.w3.org/2000/svg";

type Shape = [tag: string, attrs: Record<string, string>];

/** A 24×24 stroked icon (or filled, for the grip) from simple shapes. */
function icon(shapes: Shape[], filled = false): SVGElement {
  const svg = document.createElementNS(SVG_NS, "svg");
  const base: Record<string, string> = {
    viewBox: "0 0 24 24",
    width: "1em",
    height: "1em",
    "aria-hidden": "true",
    focusable: "false",
  };
  const paint: Record<string, string> = filled
    ? { fill: "currentColor" }
    : { fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round" };
  for (const [k, v] of Object.entries({ ...base, ...paint })) svg.setAttribute(k, v);
  for (const [tag, attrs] of shapes) {
    const node = document.createElementNS(SVG_NS, tag);
    for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
    svg.append(node);
  }
  return svg;
}

export const copyIcon = (): SVGElement =>
  icon([
    ["rect", { x: "9", y: "9", width: "13", height: "13", rx: "2" }],
    ["path", { d: "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" }],
  ]);

export const checkIcon = (): SVGElement => icon([["polyline", { points: "20 6 9 17 4 12" }]]);

export const chevronIcon = (): SVGElement => icon([["polyline", { points: "9 18 15 12 9 6" }]]);

export const folderIcon = (): SVGElement =>
  icon([["path", { d: "M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" }]]);

export const fileIcon = (): SVGElement =>
  icon([
    ["path", { d: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" }],
    ["polyline", { points: "14 2 14 8 20 8" }],
  ]);

export const editIcon = (): SVGElement => icon([["path", { d: "M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" }]]);

export const trashIcon = (): SVGElement =>
  icon([
    ["polyline", { points: "3 6 5 6 21 6" }],
    ["path", { d: "M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" }],
    ["path", { d: "M10 11v6" }],
    ["path", { d: "M14 11v6" }],
    ["path", { d: "M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" }],
  ]);

/** The six-dot drag grip grocery and todo use. */
export const gripIcon = (): SVGElement =>
  icon(
    [
      [7, 5], [17, 5], [7, 12], [17, 12], [7, 19], [17, 19],
    ].map(([cx, cy]): Shape => ["circle", { cx: String(cx), cy: String(cy), r: "2" }]),
    true,
  );
