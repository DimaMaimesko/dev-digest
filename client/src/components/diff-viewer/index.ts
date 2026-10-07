/* diff-viewer — unified-diff viewer with optional inline GitHub comments and
   findings. Public surface: the DiffViewer component + the DiffCommentApi and
   DiffFindingApi contracts. */
export { DiffViewer } from "./DiffViewer";
export type { DiffCommentApi } from "./comments";
export type { DiffFindingApi } from "./findings";
export { isOpen, pathsWithOpenFindings } from "./findings";
