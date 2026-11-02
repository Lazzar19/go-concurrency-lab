interface ChartPoint {
  x: number;
  y: number;
}

interface ChartSeries {
  label: string;
  color: string;
  data: ChartPoint[];
}

interface ChartOptions {
  xLabel?: string;
  empty?: string;
}

interface LabChartsApi {
  draw(canvas: HTMLCanvasElement, series: ChartSeries[], options?: ChartOptions): void;
}

interface Window {
  LabCharts: LabChartsApi;
}
