# GoNo

`GoNo` is a terminal-based note vault file manager written in Go, with a Bubble Tea UI.

## Features

- Select a vault from the saved list.
- Create a new vault.
- Open a vault:
  - by path (`Ctrl+O`);
  - via folder picker / explorer dialog (`Ctrl+P`, Windows).
- Navigate directories inside a vault.
- Create `.md` files (name: letters and digits only).
- Create subdirectories.
- Edit files and save (`Ctrl+S`).
- Delete files, folders, and vaults with confirmation.
- Responsive UI that adapts to terminal window size.

## Requirements

- Go `1.25+`.
- Windows, Linux, or macOS.
- Folder picker via explorer is currently implemented only for Windows.

## Run

```bash
go run .
```

Build:

```bash
go build -o gono.exe .
```

## Hotkeys

Vault selection screen:

- `Enter` - open selected vault.
- `Ctrl+N` - create vault.
- `Ctrl+O` - open vault by path.
- `Ctrl+P` - open vault via explorer (Windows).
- `Ctrl+X` - delete selected vault.
- `Ctrl+C` - quit.

Vault file screen:

- `Enter` - open folder or file.
- `Backspace` - go to parent directory.
- `Ctrl+N` - create file (`.md` is added automatically).
- `Ctrl+D` - create directory.
- `Ctrl+X` - delete selected file/directory.
- `Ctrl+C` - quit.

Editor:

- `Ctrl+S` - save file.
- `Esc` - back to file list.

Delete confirmation:

- `Y` or `Enter` - delete.
- `N` or `Esc` - cancel.

## Data Storage

- Vault registry: `~/.gono_vaults.json`.
- New vaults (created via UI) are created in the user home directory (`os.UserHomeDir()`).

## Important Notes

- Deletion is permanent (`os.Remove` / `os.RemoveAll`), no recycle bin.
- File creation and path access are restricted to the current vault (prevents path escape).
