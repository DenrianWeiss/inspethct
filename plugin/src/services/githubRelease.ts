import * as vscode from "vscode";
import * as https from "node:https";
import * as fs from "node:fs";
import * as path from "node:path";
import * as os from "node:os";

const GITHUB_API_URL = "https://api.github.com/repos/DenrianWeiss/inspethct/releases/latest";
const GITHUB_RELEASES_URL = "https://github.com/DenrianWeiss/inspethct/releases/latest";

interface GitHubRelease {
  tag_name: string;
  assets: GitHubAsset[];
}

interface GitHubAsset {
  name: string;
  browser_download_url: string;
  size: number;
}

function platformAssetName(): string | undefined {
  const platform = os.platform();
  const arch = os.arch();
  if (platform === "darwin" && arch === "arm64") {
    return "inspethctd-darwin-arm64";
  }
  if (platform === "darwin" && arch === "x64") {
    return "inspethctd-darwin-amd64";
  }
  if (platform === "linux" && arch === "arm64") {
    return "inspethctd-linux-arm64";
  }
  if (platform === "linux" && arch === "x64") {
    return "inspethctd-linux-amd64";
  }
  if (platform === "win32" && arch === "x64") {
    return "inspethctd-windows-amd64.exe";
  }
  return undefined;
}

function fetchJson<T>(url: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const req = https.get(
      url,
      {
        headers: {
          "User-Agent": "inspethct-vscode-extension",
          Accept: "application/vnd.github+json",
        },
      },
      (res) => {
        if (res.statusCode && res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          fetchJson<T>(res.headers.location).then(resolve).catch(reject);
          return;
        }
        if (res.statusCode && res.statusCode !== 200) {
          reject(new Error(`GitHub API returned ${res.statusCode}`));
          return;
        }
        let data = "";
        res.on("data", (chunk) => {
          data += chunk;
        });
        res.on("end", () => {
          try {
            resolve(JSON.parse(data) as T);
          } catch (e) {
            reject(new Error(`Failed to parse GitHub API response: ${String(e)}`));
          }
        });
      }
    );
    req.on("error", (err) => reject(err));
    req.setTimeout(15000, () => {
      req.destroy();
      reject(new Error("GitHub API request timed out"));
    });
  });
}

function downloadFile(url: string, dest: string, onProgress?: (downloaded: number, total: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    const req = https.get(
      url,
      {
        headers: {
          "User-Agent": "inspethct-vscode-extension",
        },
      },
      (res) => {
        if (res.statusCode && res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          file.close();
          fs.unlinkSync(dest);
          downloadFile(res.headers.location, dest, onProgress).then(resolve).catch(reject);
          return;
        }
        if (res.statusCode && res.statusCode !== 200) {
          file.close();
          fs.unlinkSync(dest);
          reject(new Error(`Download returned ${res.statusCode}`));
          return;
        }
        const total = parseInt(res.headers["content-length"] || "0", 10);
        let downloaded = 0;
        res.on("data", (chunk: Buffer) => {
          downloaded += chunk.length;
          onProgress?.(downloaded, total);
        });
        res.pipe(file);
        file.on("finish", () => {
          file.close(() => resolve());
        });
      }
    );
    req.on("error", (err) => {
      file.close();
      if (fs.existsSync(dest)) {
        fs.unlinkSync(dest);
      }
      reject(err);
    });
    req.setTimeout(60000, () => {
      req.destroy();
      file.close();
      if (fs.existsSync(dest)) {
        fs.unlinkSync(dest);
      }
      reject(new Error("Download timed out"));
    });
  });
}

export async function tryDownloadLatestBinary(
  context: vscode.ExtensionContext,
  token?: vscode.CancellationToken
): Promise<string | undefined> {
  const assetName = platformAssetName();
  if (!assetName) {
    void vscode.window.showWarningMessage(
      `Inspethct: No prebuilt binary available for ${os.platform()}-${os.arch()}. Please build from source.`
    );
    return undefined;
  }

  let release: GitHubRelease;
  try {
    release = await fetchJson<GitHubRelease>(GITHUB_API_URL);
  } catch (error) {
    void vscode.window.showErrorMessage(
      `Inspethct: Failed to fetch latest release info: ${String(error)}`
    );
    return undefined;
  }

  const asset = release.assets.find((a) => a.name === assetName);
  if (!asset) {
    void vscode.window.showErrorMessage(
      `Inspethct: Asset ${assetName} not found in release ${release.tag_name}.`
    );
    return undefined;
  }

  const storageDir = path.join(context.globalStorageUri.fsPath, "bin");
  fs.mkdirSync(storageDir, { recursive: true });
  const destPath = path.join(storageDir, assetName);

  const choice = await vscode.window.showInformationMessage(
    `Inspethct backend not found. Download ${release.tag_name} (${assetName}, ${formatBytes(asset.size)})?`,
    { modal: true },
    "Download",
    "View Releases",
    "Cancel"
  );

  if (choice === "View Releases") {
    void vscode.env.openExternal(vscode.Uri.parse(GITHUB_RELEASES_URL));
    return undefined;
  }
  if (choice !== "Download") {
    return undefined;
  }

  const progressOptions: vscode.ProgressOptions = {
    location: vscode.ProgressLocation.Notification,
    title: `Downloading ${assetName}…`,
    cancellable: true,
  };

  try {
    await vscode.window.withProgress(progressOptions, async (progress, cancelToken) => {
      const mergedToken: vscode.CancellationToken = {
        get isCancellationRequested() {
          return cancelToken.isCancellationRequested || (token?.isCancellationRequested ?? false);
        },
        onCancellationRequested: (listener) => {
          const d1 = cancelToken.onCancellationRequested(listener);
          const d2 = token?.onCancellationRequested(listener);
          return {
            dispose: () => {
              d1.dispose();
              d2?.dispose();
            },
          };
        },
      };

      await downloadFile(asset.browser_download_url, destPath, (downloaded, total) => {
        if (total > 0) {
          const percent = Math.round((downloaded / total) * 100);
          progress.report({ increment: percent, message: `${percent}%` });
        }
        if (mergedToken.isCancellationRequested) {
          throw new vscode.CancellationError();
        }
      });
    });
  } catch (error) {
    if (error instanceof vscode.CancellationError) {
      if (fs.existsSync(destPath)) {
        fs.unlinkSync(destPath);
      }
      return undefined;
    }
    void vscode.window.showErrorMessage(`Inspethct: Download failed: ${String(error)}`);
    return undefined;
  }

  // Make executable on Unix-like systems
  if (os.platform() !== "win32") {
    try {
      fs.chmodSync(destPath, 0o755);
    } catch (err) {
      void vscode.window.showWarningMessage(
        `Inspethct: Failed to set execute permission on ${destPath}: ${String(err)}`
      );
    }
  }

  void vscode.window.showInformationMessage(
    `Inspethct ${release.tag_name} downloaded successfully.`
  );
  return destPath;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) {
    return "0 B";
  }
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}
