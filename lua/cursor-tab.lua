local M = {}

M.ns_id = vim.api.nvim_create_namespace("cursor_tab")
M.current_suggestion = nil
M.current_suggestion_text = nil
M.current_line = nil
M.current_col = nil
M.accepting = false
M.server_url = "http://localhost:37292"
M.server_path = nil
M.server_job = nil
M.debounce_timer = nil
M.enabled = true
M.pending_job = nil

function M.setup(opts)
	opts = opts or {}

	M.server_url = opts.server_url or "http://localhost:37292"

	if not opts.server_path then
		local source = debug.getinfo(1, "S").source
		local plugin_dir = vim.fn.fnamemodify(source:sub(2), ":h:h")
		M.server_path = plugin_dir .. "/bin/cursor-tab-server"
	else
		M.server_path = opts.server_path
	end

	M.ensure_server()

	vim.api.nvim_create_autocmd({ "TextChangedI" }, {
		callback = function()
			M.show_suggestion()
		end,
	})

	vim.api.nvim_create_autocmd({ "InsertLeave" }, {
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

	vim.api.nvim_create_user_command("CursorTab", function(args)
		if args.args == "toggle" then
			M.enabled = not M.enabled
			if M.enabled then
				vim.notify("CursorTab enabled", vim.log.levels.INFO)
			else
				M.clear_suggestion()
				vim.notify("CursorTab disabled", vim.log.levels.INFO)
			end
		elseif args.args == "enable" then
			M.enabled = true
			vim.notify("CursorTab enabled", vim.log.levels.INFO)
		elseif args.args == "disable" then
			M.enabled = false
			M.clear_suggestion()
			vim.notify("CursorTab disabled", vim.log.levels.INFO)
		else
			vim.notify("Usage: :CursorTab [toggle|enable|disable]", vim.log.levels.ERROR)
		end
	end, {
		nargs = 1,
		complete = function()
			return { "toggle", "enable", "disable" }
		end,
	})
end

function M.ensure_server()
	if M.server_job and vim.fn.jobwait({ M.server_job }, 0)[1] == -1 then
		return true
	end

	if not M.server_path then
		return false
	end

	M.server_job = vim.fn.jobstart({ M.server_path }, {
		on_exit = function(_, exit_code)
			if exit_code ~= 0 then
				vim.notify("cursor-tab server exited with code " .. exit_code, vim.log.levels.ERROR)
			end
			M.server_job = nil
		end,
		on_stderr = function(_, data)
			if data and #data > 0 and data[1] ~= "" then
				vim.notify("cursor-tab server: " .. table.concat(data, "\n"), vim.log.levels.WARN)
			end
		end,
	})

	if M.server_job == 0 or M.server_job == -1 then
		vim.notify("Failed to start cursor-tab server at " .. M.server_path, vim.log.levels.ERROR)
		M.server_job = nil
		return false
	end

	vim.defer_fn(function() end, 100)
	return true
end

function M.get_suggestion(callback)
	if not M.ensure_server() then
		if callback then
			callback(nil)
		end
		return
	end

	local bufnr = vim.api.nvim_get_current_buf()
	local cursor = vim.api.nvim_win_get_cursor(0)
	local line = cursor[1] - 1
	local col = cursor[2]

	local workspace_path = vim.fn.getcwd()

	local req = {
		file_contents = table.concat(vim.api.nvim_buf_get_lines(bufnr, 0, -1, false), "\n"),
		line = line,
		column = col,
		file_path = vim.fn.expand("%:p"),
		language_id = vim.bo.filetype,
		workspace_path = workspace_path,
	}

	local json_data = vim.fn.json_encode(req)
	local tmpfile = vim.fn.tempname()

	if M.pending_job then
		vim.fn.jobstop(M.pending_job)
		M.pending_job = nil
	end

	M.pending_job = vim.fn.jobstart({
		"curl",
		"-s",
		"-X",
		"POST",
		"-H",
		"Content-Type: application/json",
		"-d",
		json_data,
		M.server_url .. "/suggestion",
	}, {
		on_stdout = function(_, data)
			if not data or #data == 0 then
				return
			end

			local response_text = table.concat(data, "\n")
			if response_text == "" then
				return
			end

			local ok, response = pcall(vim.fn.json_decode, response_text)
			if ok and response and response.suggestion then
				if callback then
					callback(response.suggestion)
				end
			else
				if callback then
					callback(nil)
				end
			end

			M.pending_job = nil
		end,
		on_exit = function()
			M.pending_job = nil
		end,
		stdout_buffered = true,
	})
end

function M.show_suggestion()
	if not M.enabled or M.accepting then
		return
	end

	if M.debounce_timer then
		vim.fn.timer_stop(M.debounce_timer)
		M.debounce_timer = nil
	end

	M.clear_suggestion()

	M.debounce_timer = vim.fn.timer_start(45, function()
		M.debounce_timer = nil

		local line = vim.api.nvim_win_get_cursor(0)[1] - 1
		local col = vim.api.nvim_win_get_cursor(0)[2]

		M.get_suggestion(function(suggestion)
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
						virt_text = { { text, "Comment" } },
						virt_text_pos = "inline",
						hl_mode = "combine",
					})
				else
					table.insert(virt_lines, { { text, "Comment" } })
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
			vim.api.nvim_buf_set_text(0, line, col, line, col, { suggestion })
			vim.api.nvim_win_set_cursor(0, { line + 1, col + #suggestion })
		else
			local before = line_text:sub(1, col)
			local after = line_text:sub(col + 1)

			lines[1] = before .. lines[1]
			lines[#lines] = lines[#lines] .. after

			vim.api.nvim_buf_set_lines(0, line, line + 1, false, lines)
			vim.api.nvim_win_set_cursor(0, { line + #lines, #lines[#lines] - #after })
		end

		M.accepting = false
	end)

	return true
end

return M
