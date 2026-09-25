// 后端绑定与模型的统一出口（生成物路径集中管理，重生成后仅此处需对齐）。
// 注意：isolatedModules 下类型再导出必须用 export type；
// 需要作为值使用（createFrom 等）的类请直接从 bindings 路径导入。
import * as bindings from "../bindings/glmquotawatch-gui/internal/guiapp/bindingsservice.js";
export { bindings };

export type { GUIState, ErrInfo } from "../bindings/glmquotawatch-gui/internal/guiapp/models.js";
export type {
  ConfigView,
  StatusView,
  WindowView,
  HistoryPoint,
  HistoryWindow,
} from "../bindings/glmquotawatch-gui/internal/service/models.js";
