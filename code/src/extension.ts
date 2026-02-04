import * as path from "path";
import {
    DebugAdapter,
    DebugAdapterDescriptor,
    DebugAdapterDescriptorFactory,
    DebugAdapterInlineImplementation,
    DebugConfiguration,
    DebugConfigurationProvider,
    DebugSession,
    EventEmitter,
    ExtensionContext,
    Terminal,
    WorkspaceFolder,
    commands,
    debug,
    window,
    workspace,
} from "vscode";
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

async function runMshellFile(filePath: string): Promise<void> {
    const document = workspace.textDocuments.find(
        (doc) => doc.uri.fsPath === filePath
    );
    if (document?.isDirty) {
        const saved = await document.save();
        if (!saved) {
            window.showErrorMessage("Unable to save the file before running.");
            return;
        }
    }

    const mshPath = getMshPath();
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

    if (isWindows && needsQuote && isWindowsPowerShell()) {
        terminal.sendText(`& ${command}`, true);
        return;
    }

    terminal.sendText(command, true);
}

function isWindowsPowerShell(): boolean {
    if (process.platform !== "win32") {
        return false;
    }
    return getWindowsShell()?.toLowerCase().includes("powershell") || false;
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

    await runMshellFile(document.uri.fsPath);
}

class MshellDebugAdapter implements DebugAdapter {
    private readonly emitter = new EventEmitter<any>();
    public readonly onDidSendMessage = this.emitter.event;

    handleMessage(message: any): void {
        if (!message || message.type !== "request") {
            return;
        }

        switch (message.command) {
            case "initialize":
                this.sendResponse(message, {
                    supportsConfigurationDoneRequest: true,
                    supportsTerminateRequest: true,
                });
                this.sendEvent("initialized");
                return;
            case "configurationDone":
                this.sendResponse(message);
                return;
            case "launch": {
                const program = message.arguments?.program as string | undefined;
                if (!program) {
                    this.sendError(
                        message,
                        "No program configured. Set 'program' in the launch configuration."
                    );
                    this.sendEvent("terminated");
                    return;
                }
                void runMshellFile(program);
                this.sendResponse(message);
                this.sendEvent("terminated");
                this.sendEvent("exited", { exitCode: 0 });
                return;
            }
            case "disconnect":
            case "terminate":
                this.sendResponse(message);
                this.sendEvent("terminated");
                return;
            case "threads":
                this.sendResponse(message, {
                    threads: [{ id: 1, name: "mshell" }],
                });
                return;
            default:
                this.sendResponse(message);
        }
    }

    dispose(): void {
        this.emitter.dispose();
    }

    private sendResponse(request: any, body?: any): void {
        this.emitter.fire({
            type: "response",
            request_seq: request.seq,
            success: true,
            command: request.command,
            body,
        });
    }

    private sendError(request: any, message: string): void {
        this.emitter.fire({
            type: "response",
            request_seq: request.seq,
            success: false,
            command: request.command,
            message,
        });
    }

    private sendEvent(event: string, body?: any): void {
        this.emitter.fire({
            type: "event",
            event,
            body,
        });
    }
}

class MshellDebugAdapterFactory implements DebugAdapterDescriptorFactory {
    createDebugAdapterDescriptor(
        _session: DebugSession
    ): DebugAdapterDescriptor {
        return new DebugAdapterInlineImplementation(new MshellDebugAdapter());
    }
}

class MshellDebugConfigurationProvider implements DebugConfigurationProvider {
    resolveDebugConfiguration(
        _folder: WorkspaceFolder | undefined,
        config: DebugConfiguration
    ): DebugConfiguration | undefined {
        if (!config.type && !config.request && !config.name) {
            config.type = "mshell";
            config.request = "launch";
            config.name = "Run mshell file";
        }

        if (!config.program) {
            const editor = window.activeTextEditor;
            if (!editor || editor.document.languageId !== "mshell") {
                window.showErrorMessage("Open an mshell file to run.");
                return undefined;
            }
            if (editor.document.isUntitled) {
                window.showErrorMessage("Save the file before running.");
                return undefined;
            }
            config.program = editor.document.uri.fsPath;
        }

        return config;
    }
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

    const debugFactory = debug.registerDebugAdapterDescriptorFactory(
        "mshell",
        new MshellDebugAdapterFactory()
    );
    const debugConfigProvider = debug.registerDebugConfigurationProvider(
        "mshell",
        new MshellDebugConfigurationProvider()
    );

    context.subscriptions.push(runCommand, debugFactory, debugConfigProvider);
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
