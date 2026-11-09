export interface StartupItem {
  id: string;
  source: string;
  name: string;
  raw_command: string;
  exe_path: string;
  args: string;
  is_enabled: boolean;
  file_exists: boolean;
  publisher: string;
  is_elevated: boolean;

  // 计划任务扩展字段（注册表 / 启动文件夹来源为空）
  trigger_desc?: string;
  task_path?: string;
  next_run?: string;
  last_run?: string;
  last_result?: number;
}