// icons.ts — the small stroked icon set the agentflow buttons share. Nodes are
// built with createElementNS (never innerHTML) and paint with currentColor, so
// every theme colours them. Sized 1em; the owning button carries the label.

const SVG_NS = 'http://www.w3.org/2000/svg';

type Shape = [tag: string, attrs: Record<string, string>];

const line = (x1: number, y1: number, x2: number, y2: number): Shape => ['line', { x1: String(x1), y1: String(y1), x2: String(x2), y2: String(y2) }];
const poly = (points: string): Shape => ['polyline', { points }];
const path = (d: string): Shape => ['path', { d }];

const SHAPES = {
  save: [path('M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z'), poly('17 21 17 13 7 13 7 21'), poly('7 3 7 8 15 8')],
  add: [line(12, 5, 12, 19), line(5, 12, 19, 12)],
  remove: [line(6, 6, 18, 18), line(18, 6, 6, 18)],
  trash: [poly('3 6 5 6 21 6'), path('M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6'), path('M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2')],
  edit: [path('M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z')],
  cancel: [line(6, 6, 18, 18), line(18, 6, 6, 18)],
  approve: [poly('20 6 9 17 4 12')],
  start: [poly('6 4 20 12 6 20')],
  pause: [line(8, 5, 8, 19), line(16, 5, 16, 19)],
  stop: [['rect', { x: '6', y: '6', width: '12', height: '12', rx: '1' }]],
  retry: [poly('1 4 1 10 7 10'), path('M3.51 15a9 9 0 1 0 2.13-9.36L1 10')],
  refresh: [poly('23 4 23 10 17 10'), poly('1 20 1 14 7 14'), path('M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15')],
  copy: [['rect', { x: '9', y: '9', width: '13', height: '13', rx: '2' }], path('M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1')],
  up: [line(12, 19, 12, 5), poly('5 12 12 5 19 12')],
  folder: [path('M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z')],
  probe: [['circle', { cx: '11', cy: '11', r: '8' }], line(21, 21, 16.65, 16.65)],
  view: [path('M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z'), ['circle', { cx: '12', cy: '12', r: '3' }]],
  import: [path('M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4'), poly('17 8 12 3 7 8'), line(12, 3, 12, 15)],
} satisfies Record<string, Shape[]>;

export type IconName = keyof typeof SHAPES;

/** icon builds the named glyph as a decorative inline SVG. */
export function icon(name: IconName): SVGElement {
  const svg = document.createElementNS(SVG_NS, 'svg');
  const attrs: Record<string, string> = {
    viewBox: '0 0 24 24', width: '1em', height: '1em', 'aria-hidden': 'true', focusable: 'false',
    fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round',
  };
  for (const [k, v] of Object.entries(attrs)) svg.setAttribute(k, v);
  for (const [tag, a] of SHAPES[name]) {
    const node = document.createElementNS(SVG_NS, tag);
    for (const [k, v] of Object.entries(a)) node.setAttribute(k, v);
    svg.append(node);
  }
  return svg;
}
