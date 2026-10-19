import { createContext, useContext } from "react";
import type { ConsoleAPI } from "./console";
export const LiveContext = createContext<ConsoleAPI | null>(null);
export const useLive = () => useContext(LiveContext);
