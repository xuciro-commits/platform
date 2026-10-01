import { createContext, useContext } from "react";

// A hidden tab can keep its component state, but must release global overlays.
export const ViewVisibilityContext = createContext(true);
export const useViewVisible = () => useContext(ViewVisibilityContext);
