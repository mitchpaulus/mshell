import * as path from "path";
import { ExtensionContext, Terminal, commands, window, workspace } from "vscode";
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;
let runTerminal: Terminal | undefined;
let runTerminalCwd: string | undefined;

function getWindowsShell(): string | undefined {
    const terminalConfig = workspace.getConfiguration("terminal.integrated");
    return terminalConfig.get<string>("defaultProfile.windows");
}

function quotePosix(value: string): string {
    if (value.length === 0) {
        return "''";
    }
    return `'${value.replace(/'/g, `'\\''`)}'`;
}

function quoteWindows(value: string): string {
    return `"${value.replace(/"/g, '""')}"`;
}

function getMshPath(): string {
    const config = workspace.getConfiguration("mshell");
    return (
        config.get<string>("mshPath") ||
        config.get<string>("lspPath") ||
        "msh"
    );
}

async function runCurrentFile(): Promise<void> {
    const editor = window.activeTextEditor;
    if (!editor) {
        window.showErrorMessage("No active editor to run.");
        return;
    }

    const document = editor.document;
    if (document.languageId !== "mshell") {
        window.showErrorMessage("The active file is not an mshell file.");
        return;
    }

    if (document.isUntitled) {
        window.showErrorMessage("Save the file before running.");
        return;
    }

    if (document.isDirty) {
        const saved = await document.save();
        if (!saved) {
            window.showErrorMessage("Unable to save the file before running.");
            return;
        }
    }

    const mshPath = getMshPath();
    const filePath = document.uri.fsPath;
    const cwd = path.dirname(filePath);

    if (runTerminal && runTerminalCwd !== cwd) {
        runTerminal.dispose();
        runTerminal = undefined;
    }

    const terminal =
        runTerminal ||
        window.createTerminal({
            name: "mshell",
            cwd,
        });
    runTerminal = terminal;
    runTerminalCwd = cwd;

    terminal.show(true);

    const isWindows = process.platform === "win32";
    const fileArg = isWindows ? quoteWindows(filePath) : quotePosix(filePath);
    const needsQuote = isWindows && /[\s"]/g.test(mshPath);
    const commandPath = needsQuote
        ? (isWindows ? quoteWindows(mshPath) : quotePosix(mshPath))
        : mshPath;
    const command = `${commandPath} ${fileArg}`;

    if (
        isWindows &&
        needsQuote &&
        getWindowsShell()?.toLowerCase().includes("powershell")
    ) {
        terminal.sendText(`& ${command}`, true);
        return;
    }

    terminal.sendText(command, true);
}

export function activate(context: ExtensionContext): void {
    const config = workspace.getConfiguration("mshell");
    const serverPath = config.get<string>("lspPath") || "msh";

    const serverOptions: ServerOptions = {
        command: serverPath,
        args: ["lsp"],
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [{ scheme: "file", language: "mshell" }],
    };

    client = new LanguageClient(
        "mshell",
        "mshell Language Server",
        serverOptions,
        clientOptions
    );

    client.start();

    const runCommand = commands.registerCommand(
        "mshell.runCurrentFile",
        runCurrentFile
    );

    context.subscriptions.push(runCommand);
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
