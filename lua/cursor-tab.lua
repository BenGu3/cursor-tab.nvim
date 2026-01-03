local M = {}

M.ns_id = vim.api.nvim_create_namespace("cursor_tab")
M.current_suggestion = nil
M.current_suggestion_text = nil
M.current_line = nil
M.current_col = nil
M.accepting = false
M.fetching = false
M.rpc_channel = nil
M.server_path = nil

function M.setup(opts)
	opts = opts or {}

	if not opts.server_path then
		local source = debug.getinfo(1, "S").source
		local plugin_dir = vim.fn.fnamemodify(source:sub(2), ":h:h")
		M.server_path = plugin_dir .. "/bin/cursor-tab-server"
	else
		M.server_path = opts.server_path
	end

	vim.api.nvim_create_autocmd({"TextChangedI", "CursorMovedI"}, {
		callback = function()
			M.show_suggestion()
		end,
	})

	vim.api.nvim_create_autocmd({"InsertLeave"}, {
		callback = function()
			M.clear_suggestion()
		end,
	})

	vim.keymap.set("i", "<Tab>", function()
		if M.accept_suggestion() then
			return ""
		else
			return "\t"
		end
	end, { noremap = true, silent = true, expr = true })
end

function M.ensure_rpc_server()
	if M.rpc_channel and vim.fn.jobwait({M.rpc_channel}, 0)[1] == -1 then
		return M.rpc_channel
	end

	local cmd = M.server_path or "cursor-tab-server"
	M.rpc_channel = vim.fn.jobstart({cmd}, {
		rpc = true,
		on_exit = function(_, exit_code)
			if exit_code ~= 0 then
				vim.notify("cursor-tab server exited with code " .. exit_code, vim.log.levels.ERROR)
			end
			M.rpc_channel = nil
		end,
		on_stderr = function(_, data)
			if data and #data > 0 then
				vim.notify("cursor-tab server error: " .. table.concat(data, "\n"), vim.log.levels.ERROR)
			end
		end,
	})

	if M.rpc_channel == 0 then
		vim.notify("Failed to start cursor-tab server. Make sure it's built and in PATH.", vim.log.levels.ERROR)
		M.rpc_channel = nil
		return nil
	elseif M.rpc_channel == -1 then
		vim.notify("Invalid arguments for cursor-tab server", vim.log.levels.ERROR)
		M.rpc_channel = nil
		return nil
	end

	return M.rpc_channel
end

function M.get_suggestion(callback)
	local channel = M.ensure_rpc_server()
	if not channel then
		if callback then callback(nil) end
		return
	end

	local bufnr = vim.api.nvim_get_current_buf()
	local cursor = vim.api.nvim_win_get_cursor(0)
	local line = cursor[1] - 1
	local col = cursor[2]

	local req = {
		file_contents = table.concat(vim.api.nvim_buf_get_lines(bufnr, 0, -1, false), "\n"),
		line = line,
		column = col,
		file_path = vim.fn.expand("%:p"),
		language_id = vim.bo.filetype,
	}

	vim.schedule(function()
		local success, result = pcall(vim.rpcrequest, channel, "get_suggestion", req)

		if success and result and result ~= "" then
			if callback then
				callback(result)
			end
		else
			if callback then
				callback(nil)
			end
		end
	end)
end

function M.show_suggestion()
	if M.accepting or M.fetching then
		return
	end

	M.clear_suggestion()
	M.fetching = true

	local line = vim.api.nvim_win_get_cursor(0)[1] - 1
	local col = vim.api.nvim_win_get_cursor(0)[2]

	M.get_suggestion(function(suggestion)
		M.fetching = false

		if not suggestion then
			return
		end

		M.clear_suggestion()
		M.current_suggestion_text = suggestion

		local lines = vim.split(suggestion, "\n", { plain = true })
		local virt_lines = {}

		for i, text in ipairs(lines) do
			if i == 1 then
				M.current_suggestion = vim.api.nvim_buf_set_extmark(0, M.ns_id, line, col, {
					virt_text = {{text, "Comment"}},
					virt_text_pos = "inline",
					hl_mode = "combine",
				})
			else
				table.insert(virt_lines, {{text, "Comment"}})
			end
		end

		if #virt_lines > 0 then
			vim.api.nvim_buf_set_extmark(0, M.ns_id, line, col, {
				virt_lines = virt_lines,
				virt_lines_above = false,
			})
		end

		M.current_line = line
		M.current_col = col
	end)
end

function M.clear_suggestion()
	if M.current_suggestion then
		vim.api.nvim_buf_clear_namespace(0, M.ns_id, 0, -1)
		M.current_suggestion = nil
		M.current_suggestion_text = nil
		M.current_line = nil
		M.current_col = nil
	end
end

function M.accept_suggestion()
	if not M.current_suggestion or M.accepting or not M.current_suggestion_text then
		return false
	end

	local line = vim.api.nvim_win_get_cursor(0)[1] - 1
	local col = vim.api.nvim_win_get_cursor(0)[2]
	local line_text = vim.api.nvim_get_current_line()
	local suggestion = M.current_suggestion_text

	M.accepting = true
	M.clear_suggestion()

	vim.schedule(function()
		local lines = vim.split(suggestion, "\n", { plain = true })

		if #lines == 1 then
			vim.api.nvim_buf_set_text(0, line, col, line, col, {suggestion})
			vim.api.nvim_win_set_cursor(0, {line + 1, col + #suggestion})
		else
			local before = line_text:sub(1, col)
			local after = line_text:sub(col + 1)

			lines[1] = before .. lines[1]
			lines[#lines] = lines[#lines] .. after

			vim.api.nvim_buf_set_lines(0, line, line + 1, false, lines)
			vim.api.nvim_win_set_cursor(0, {line + #lines, #lines[#lines] - #after})
		end

		M.accepting = false
	end)

	return true
end

return M
