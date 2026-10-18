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
}