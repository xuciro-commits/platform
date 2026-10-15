import type { ReactNode } from "react";
import { Boxes, Braces, ClipboardCheck, Database, Layers, LayoutTemplate, Link2, Search, Sparkles, Tags, Workflow } from "lucide-react";

export const kindIcon: Record<string, ReactNode> = {
  object: <Boxes />, "link-type": <Link2 />, "property-type": <Tags />, query: <Search />, page: <LayoutTemplate />, flow: <Workflow />, function: <Sparkles />, agent: <Sparkles />, "evaluation-set": <Sparkles />, compute: <Braces />,
  observation: <Database />, connection: <Link2 />, source: <Database />, dataset: <Database />, pipeline: <Workflow />, writeback: <Link2 />, matching: <Layers />,
  "test-plan": <ClipboardCheck />,
};
