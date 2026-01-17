# cursor-tab.nvim

⚠️ **Experimental** - Very loosely tested, under active development. Use at your own risk.

Brings Cursor's AI-powered tab completion to Neovim. Get code suggestions as you type and accept them with Tab, just like in Cursor IDE.

## Requirements

**System:**
- **macOS only** (reads Cursor's auth from macOS-specific paths)
- curl (for HTTP requests and binary download)
- sqlite3 (to read Cursor credentials)

**Critical:**
- **Cursor IDE must be installed** at `/Applications/Cursor.app`
- **You must be signed into Cursor** (plugin reads your auth token automatically)

Without Cursor installed and authenticated, the plugin won't work.

## Installation

### lazy.nvim
```lua
{
  "bengu3/cursor-tab.nvim",
  config = function()
    require("cursor-tab").setup()
  end,
}
```

The plugin will automatically download the appropriate binary for your platform on first run.

### packer.nvim
```lua
use {
  "bengu3/cursor-tab.nvim",
  config = function()
    require("cursor-tab").setup()
  end
}
```

### vim-plug
```vim
Plug 'bengu3/cursor-tab.nvim'
```

Add to `init.lua`:
```lua
require("cursor-tab").setup()
```

### Manual Binary Installation

If auto-download fails, you can manually install:
1. Download the binary for your platform from [Releases](https://github.com/bengu3/cursor-tab.nvim/releases/latest)
2. Place it at `~/.local/share/nvim/lazy/cursor-tab.nvim/bin/cursor-tab-server` (or equivalent path for your plugin manager)
3. Make it executable: `chmod +x path/to/cursor-tab-server`

Or run `:CursorTabInstall` in Neovim to retry auto-installation.

### Custom Server Path (Optional)
```lua
require("cursor-tab").setup({
  server_path = "/custom/path/to/cursor-tab-server"
})
```

### Building from Source (Optional)

If you prefer to build from source:

```bash
# Requirements: Go 1.21+, buf CLI, make
git clone https://github.com/bengu3/cursor-tab.nvim ~/.config/nvim/pack/plugins/start/cursor-tab.nvim
cd ~/.config/nvim/pack/plugins/start/cursor-tab.nvim
make build
```

## Usage

1. Open any file in Neovim
2. Start typing in insert mode
3. After ~150ms, AI suggestion appears as dimmed text
4. Press `<Tab>` to accept, or keep typing to dismiss
5. Use `:CursorTab toggle` to enable/disable

That's it! The Go server starts automatically in the background.
