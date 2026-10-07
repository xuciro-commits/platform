// Code 128 (subset B) as inline SVG, for labels printed from a record page
// (ADR-0057 D1). Pure function of the text: no library, no canvas.
const patterns = ["212222","222122","222221","121223","121322","131222","122213","122312","132212","221213","221312","231212","112232","122132","122231","113222","123122","123221","223211","221132","221231","213212","223112","312131","311222","321122","321221","312212","322112","322211","212123","212321","232121","111323","131123","131321","112313","132113","132311","211313","231113","231311","112133","112331","132131","113123","113321","133121","313121","211331","231131","213113","213311","213131","311123","311321","331121","312113","312311","332111","314111","221411","431111","111224","111422","121124","121421","141122","141221","112214","112412","122114","122411","142112","142211","241211","221114","413111","241112","134111","111242","121142","121241","114212","124112","124211","411212","421112","421211","212141","214121","412121","111143","111341","131141","114113","114311","411113","411311","113141","114131","311141","411131","211412","211214","211232","2331112"];
const startB = 104, stop = 106;

/** The bar widths of a Code 128 B symbol encoding text, or nothing for text it cannot encode. */
function code128(text: string): number[] | undefined {
  if (!text || [...text].some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) > 126)) return undefined;
  const codes = [startB, ...[...text].map((c) => c.charCodeAt(0) - 32)];
  const check = codes.reduce((sum, code, i) => sum + code * Math.max(i, 1), 0) % 103;
  return [...codes, check, stop].flatMap((code) => [...patterns[code]!].map(Number));
}

export function Barcode({ text, height = 48, className }: { text: string; height?: number; className?: string }) {
  const widths = code128(text);
  if (!widths) return null;
  const total = widths.reduce((a, b) => a + b, 0) + 20; // quiet zones of 10 modules
  let x = 10;
  const bars = widths.map((w, i) => { const bar = i % 2 === 0 ? <rect key={i} x={x} y={0} width={w} height={height} /> : null; x += w; return bar; });
  return <svg role="img" aria-label={text} className={className} viewBox={`0 0 ${total} ${height + 14}`} width={total * 2} height={(height + 14) * 2} shapeRendering="crispEdges" fill="currentColor">
    {bars}<text x={total / 2} y={height + 11} textAnchor="middle" fontFamily="ui-monospace, monospace" fontSize={10}>{text}</text>
  </svg>;
}
