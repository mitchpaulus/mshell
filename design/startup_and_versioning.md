I have these goals for our versioning system and startup:

1. We are going to have the concept of specifying the corresponding mshell version in the file itself.

   ```msh
   VER "v0.11.0"
   ```

   This implies a corresponding compiler and standard library code.

2. Startup convention and overriding.

   On startup, mshell should be reading in:

   1. The appropriate standard library code
   2. A user-specific initialization file
   3. A directory of snippets for third-party software to put things like completions (FUTURE)


## Versioning

The versioning should correspond to git tags.
If the version does not match the internal version of the current executing `mshell`,
it will attempt to find/install on the system.

## Startup

We should be taking these steps when executing a file:

1. Parse file and look for version in the file.

   - No version:
     - Look for std library code at:
       - `$XDG_DATA_HOME/msh/lib/{version}/std.msh` (Unix) or `$LOCALAPPDATA/msh/lib/{version}/std.msh` (Windows)
     - Look for user-init code at:
       - `$XDG_CONFIG_HOME/msh/init/init.msh` (Unix) or `$LOCALAPPDATA/msh/init/init.msh` (Windows)

     - Fail if these files are not found and explain to user.

   - Has version:
     - Look for std library code at:
       - `$XDG_DATA_HOME/msh/lib/{version}/std.msh` (Unix) or `$LOCALAPPDATA/msh/lib/{version}/std.msh` (Windows)
     - Look for user-init code at:
       - `$XDG_CONFIG_HOME/msh/init/{version}/init.msh` (Unix) or `$LOCALAPPDATA/msh/init/{version}/init.msh` (Windows)

     - If these are not found, prompt user if they want to download them from the internet to get them.

For interactive use, we are taking the version of the current executable.
