// ECharts 6, loaded the first time a chart draws (ADR-0019 D5): the kit's other
// pages never download it.
import { BarChart, LineChart, PieChart, ScatterChart } from "echarts/charts";
import { GridComponent, LegendComponent, TooltipComponent } from "echarts/components";
import { init, use } from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

use([BarChart, LineChart, PieChart, ScatterChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

export { init };
