# cursor-tab.nvim

⚠️ **Experimental** - Very loosely tested, under active development. Use at your own risk.

Brings Cursor's AI-powered tab completion to Neovim. Get code suggestions as you type and accept them with Tab, just like in Cursor IDE.

## Requirements

**System:**
- **macOS only** (reads Cursor's auth from macOS-specific paths)
- Go 1.21+ to build the server
- curl (for HTTP requests)
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
  build = "make build",
  config = function()
    require("cursor-tab").setup()
  end,
}
```

### packer.nvim
```lua
use {
  "bengu3/cursor-tab.nvim",
  run = "make build",
  config = function()
    require("cursor-tab").setup()
  end
}
```

### vim-plug
```vim
Plug 'bengu3/cursor-tab.nvim'
```
Then run `:!make build` in the plugin directory.

### Manual
```bash
git clone https://github.com/bengu3/cursor-tab.nvim ~/.config/nvim/pack/plugins/start/cursor-tab.nvim
cd ~/.config/nvim/pack/plugins/start/cursor-tab.nvim
make build
```

Add to `init.lua`:
```lua
require("cursor-tab").setup()
```

### Custom Server Path (Optional)
```lua
require("cursor-tab").setup({
  server_path = "/custom/path/to/cursor-tab-server"
})
```

## Usage

1. Open any file in Neovim
2. Start typing in insert mode
3. After ~150ms, AI suggestion appears as dimmed text
4. Press `<Tab>` to accept, or keep typing to dismiss
5. Use `:CursorTab toggle` to enable/disable

That's it! The Go server starts automatically in the background.
