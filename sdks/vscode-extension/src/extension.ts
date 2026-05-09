// Gateway-LLM VS Code / Cursor extension.
//
// Goal: turn every editor into a Gateway-LLM dashboard. The extension
// adds a status-bar item showing today's spend, three commands for
// common audits (latest spend, open recording, verify receipt), and a
// webview for viewing recordings inline.
//
// It never makes writes; all mutations go through the gateway itself.
import * as vscode from "vscode";

interface Telemetry {
  date: string;
  total_tokens: number;
  total_cost: number;
}

async function fetchJSON<T>(base: string, path: string, apiKey: string): Promise<T> {
  const resp = await fetch(`${base.replace(/\/$/, "")}${path}`, {
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  if (!resp.ok) throw new Error(`${path}: ${resp.status}`);
  return (await resp.json()) as T;
}

export function activate(ctx: vscode.ExtensionContext) {
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.text = "$(flame) gateway-llm: $0.00";
  status.tooltip = "Today's spend on Gateway-LLM";
  status.command = "gatewayLLM.showLatestSpend";
  status.show();
  ctx.subscriptions.push(status);

  const refresh = async () => {
    const cfg = vscode.workspace.getConfiguration("gatewayLLM");
    const base = cfg.get<string>("baseURL") ?? "";
    const key = cfg.get<string>("apiKey") ?? "";
    if (!base || !key) return;
    try {
      const rows = await fetchJSON<{ daily: Telemetry[] }>(base, "/v1/management/usage/daily", key);
      const today = rows.daily?.[0];
      if (today) status.text = `$(flame) gateway-llm: $${today.total_cost.toFixed(2)}`;
    } catch {
      status.text = "$(flame) gateway-llm: offline";
    }
  };
  const timer = setInterval(refresh, 30_000);
  ctx.subscriptions.push({ dispose: () => clearInterval(timer) });
  refresh();

  ctx.subscriptions.push(
    vscode.commands.registerCommand("gatewayLLM.showLatestSpend", async () => {
      await refresh();
      vscode.window.showInformationMessage(status.text);
    })
  );

  ctx.subscriptions.push(
    vscode.commands.registerCommand("gatewayLLM.openRecording", async () => {
      const id = await vscode.window.showInputBox({ prompt: "Recording / trace id" });
      if (!id) return;
      const cfg = vscode.workspace.getConfiguration("gatewayLLM");
      const base = cfg.get<string>("baseURL") ?? "";
      const key = cfg.get<string>("apiKey") ?? "";
      try {
        const rec = await fetchJSON<any>(base, `/v1/recordings/${id}`, key);
        const doc = await vscode.workspace.openTextDocument({
          language: "json",
          content: JSON.stringify(rec, null, 2),
        });
        await vscode.window.showTextDocument(doc);
      } catch (e: any) {
        vscode.window.showErrorMessage(`gateway-llm: ${e.message}`);
      }
    })
  );

  ctx.subscriptions.push(
    vscode.commands.registerCommand("gatewayLLM.verifyReceipt", async () => {
      const id = await vscode.window.showInputBox({ prompt: "Receipt id" });
      if (!id) return;
      const cfg = vscode.workspace.getConfiguration("gatewayLLM");
      const base = cfg.get<string>("baseURL") ?? "";
      const key = cfg.get<string>("apiKey") ?? "";
      try {
        const rcpt = await fetchJSON<any>(base, `/v1/receipts/${id}`, key);
        vscode.window.showInformationMessage(
          `Receipt ${rcpt.id} — $${(rcpt.cost_usd ?? 0).toFixed(6)} — ${rcpt.total_tokens} tokens`
        );
      } catch (e: any) {
        vscode.window.showErrorMessage(`gateway-llm: ${e.message}`);
      }
    })
  );
}

export function deactivate() { /* nothing to clean up */ }
