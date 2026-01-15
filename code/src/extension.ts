import * as path from "path";
import { ExtensionContext, workspace } from "vscode";
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;

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
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
