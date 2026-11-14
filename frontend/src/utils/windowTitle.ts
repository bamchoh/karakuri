// ウィンドウのタイトルバーに出す文字。
// プロジェクトを開いている（保存した）ときは、そのファイル名を先に出す。

// パスの最後の部分（ファイル名）。区切りは / と \ のどちらでもよい
export function fileNameOf(path: string): string {
  const parts = path.split(/[\\/]/).filter((part) => part !== "");
  return parts.length > 0 ? parts[parts.length - 1] : "";
}

export function windowTitle(version: string, projectPath: string): string {
  const app = `Karakuri ${version}`;
  const name = fileNameOf(projectPath);
  return name ? `${name} - ${app}` : app;
}
