import {analysisServicePlugins} from "./analysis-service-plugins";
import {collaborationPlugins} from "./collaboration-plugins";
import {authoringPlugins} from "./authoring-plugins";
import {listingPlugins} from "./listing-plugins";
import {executionPlugins} from "./execution-plugins";
import {detailPlugins} from "./detail-plugins";
import {chartPlugins} from "./chart-plugins";
import {recordPresentationPlugins} from "./record-presentation-plugins";
import {surfacePlugins} from "./surface-plugins";
import {distributionPlugins} from "./distribution-plugins";
import {navigationPlugins} from "./navigation-plugins";
import {scalarPlugins} from "./scalar-plugins";
import {recordWindowPlugins} from "./record-window-plugins";
import {contentPlugins} from "./content-plugins";
import {inputPlugins} from "./input-plugins";
import {createWidgetRegistry} from "./registry";
import type {WidgetBindingContext} from "./bindings";

export const pageWidgetRegistry = createWidgetRegistry<WidgetBindingContext>({
 ...inputPlugins,
 ...contentPlugins,
 ...scalarPlugins,
 ...recordWindowPlugins,
 ...distributionPlugins,
 ...navigationPlugins,
 ...recordPresentationPlugins,
 ...surfacePlugins,
 ...detailPlugins,
 ...chartPlugins,
 ...listingPlugins,
 ...executionPlugins,
 ...authoringPlugins,
 ...analysisServicePlugins,
 ...collaborationPlugins,
});
