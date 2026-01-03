-- Auto-setup cursor-tab plugin
if vim.g.loaded_cursor_tab then
	return
end
vim.g.loaded_cursor_tab = 1

-- Require and setup the plugin
require("cursor-tab").setup()
