import { createContext, useContext } from "react";

export const ViewVisibilityContext = createContext(true);
export const useViewVisible = () => useContext(ViewVisibilityContext);
