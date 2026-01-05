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
					callback(response.suggestion, response.range_replace)
				end
			else
				if callback then
					callback(nil, nil)
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

		M.get_suggestion(function(suggestion, range_replace)
			if not suggestion then
				return
			end

			-- Strip carriage returns (Windows line endings)
			suggestion = suggestion:gsub("\r", "")

			-- Re-check cursor position and validate it hasn't changed
			local current_line = vim.api.nvim_win_get_cursor(0)[1] - 1
			local current_col = vim.api.nvim_win_get_cursor(0)[2]

			-- Validate the position is still valid
			if current_line ~= line or current_col ~= col then
				return -- Cursor moved, discard this suggestion
			end

			-- Validate column is within line bounds
			local bufnr = vim.api.nvim_get_current_buf()
			local line_text = vim.api.nvim_buf_get_lines(bufnr, line, line + 1, false)[1]
			if not line_text or col > #line_text then
				return -- Invalid position
			end

			M.clear_suggestion()
			M.current_suggestion_text = suggestion
			M.current_range_replace = range_replace

			-- If we have a range to replace, calculate what to display
			local display_text = suggestion
			local display_line = line
			local display_col = col

			if range_replace then
				-- LineRange only has line numbers, use cursor position for column precision
				local bufnr = vim.api.nvim_get_current_buf()
				-- API returns 1-indexed line numbers, convert to 0-indexed
				local start_line = range_replace.start_line - 1
				local end_line = range_replace.end_line - 1

				-- Use the range's line for display, but validate it first
				-- If the range extends beyond current buffer, just use request line
				local line_count = vim.api.nvim_buf_line_count(bufnr)
				local range_out_of_bounds = false
				if start_line >= 0 and start_line < line_count then
					display_line = start_line
				else
					display_line = line
					range_out_of_bounds = true
				end

				-- For single-line replacements on current line, use cursor position
				-- Also apply if range was out of bounds (treat as same-line)
				if (start_line == line and end_line == line) or range_out_of_bounds then
					-- Strip leading newline if present (API sometimes includes it)
					local clean_suggestion = suggestion
					if vim.startswith(clean_suggestion, "\n") then
						clean_suggestion = string.sub(clean_suggestion, 2)
					end

					-- Get text from start of line to cursor (use display_line, not start_line)
					local current_line_text = vim.api.nvim_buf_get_lines(bufnr, display_line, display_line + 1, false)[1] or ""
					local replaced_text = string.sub(current_line_text, 1, col)

					-- If suggestion starts with the replaced text, strip it for display
					if vim.startswith(clean_suggestion, replaced_text) then
						display_text = string.sub(clean_suggestion, #replaced_text + 1)
					else
						display_text = clean_suggestion
					end
				end
				-- For multi-line replacements, show full suggestion
			end

			-- Validate display position is within bounds
			local bufnr = vim.api.nvim_get_current_buf()
			local line_count = vim.api.nvim_buf_line_count(bufnr)
			if display_line < 0 or display_line >= line_count then
				return
			end

			local display_line_text = vim.api.nvim_buf_get_lines(bufnr, display_line, display_line + 1, false)[1] or ""
			if display_col > #display_line_text then
				return
			end

			local lines = vim.split(display_text, "\n", { plain = true })
			local virt_lines = {}

			-- If suggestion starts with newline, first line will be empty
			-- In that case, show everything as virt_lines below current line
			if #lines > 0 and lines[1] == "" then
				-- Skip empty first line, show rest as virt_lines
				for i = 2, #lines do
					table.insert(virt_lines, { { lines[i], "Comment" } })
				end
				if #virt_lines > 0 then
					M.current_suggestion = vim.api.nvim_buf_set_extmark(0, M.ns_id, display_line, display_col, {
						virt_lines = virt_lines,
						virt_lines_above = false,
					})
				end
			else
				-- Normal case: first line inline, rest as virt_lines
				for i, text in ipairs(lines) do
					if i == 1 then
						M.current_suggestion = vim.api.nvim_buf_set_extmark(0, M.ns_id, display_line, display_col, {
							virt_text = { { text, "Comment" } },
							virt_text_pos = "inline",
							hl_mode = "combine",
						})
					else
						table.insert(virt_lines, { { text, "Comment" } })
					end
				end

				if #virt_lines > 0 then
					vim.api.nvim_buf_set_extmark(0, M.ns_id, display_line, display_col, {
						virt_lines = virt_lines,
						virt_lines_above = false,
					})
				end
			end

			M.current_line = display_line
			M.current_col = display_col
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
	local suggestion = M.current_suggestion_text
	local range_replace = M.current_range_replace

	M.accepting = true
	M.clear_suggestion()

	vim.schedule(function()
		local lines = vim.split(suggestion, "\n", { plain = true })

		-- If we have a range to replace, handle it
		if range_replace then
			-- API returns 1-indexed line numbers, convert to 0-indexed
			local start_line = range_replace.start_line - 1
			local end_line = range_replace.end_line - 1

			-- Validate range is within bounds
			local bufnr = vim.api.nvim_get_current_buf()
			local line_count = vim.api.nvim_buf_line_count(bufnr)
			local range_out_of_bounds = start_line < 0 or start_line >= line_count

			-- For same-line replacement or out-of-bounds range, use cursor position
			if (start_line == line and end_line == line) or range_out_of_bounds then
				-- Strip leading newline if present (like we do for display)
				local clean_suggestion = suggestion
				if vim.startswith(clean_suggestion, "\n") then
					clean_suggestion = string.sub(clean_suggestion, 2)
				end

				local clean_lines = vim.split(clean_suggestion, "\n", { plain = true })

				-- Replace from beginning of line to cursor with suggestion
				local line_text = vim.api.nvim_buf_get_lines(0, line, line + 1, false)[1] or ""
				local after = line_text:sub(col + 1)

				if #clean_lines == 1 then
					vim.api.nvim_buf_set_text(0, line, 0, line, col, { clean_suggestion })
					vim.api.nvim_win_set_cursor(0, { line + 1, #clean_suggestion })
				else
					clean_lines[#clean_lines] = clean_lines[#clean_lines] .. after
					vim.api.nvim_buf_set_lines(0, line, line + 1, false, clean_lines)
					vim.api.nvim_win_set_cursor(0, { line + #clean_lines, #clean_lines[#clean_lines] - #after })
				end
			else
				-- Multi-line replacement: replace entire lines
				vim.api.nvim_buf_set_lines(0, start_line, end_line + 1, false, lines)
				vim.api.nvim_win_set_cursor(0, { start_line + #lines, #lines[#lines] })
			end
		else
			-- No range to replace, just insert at cursor
			local line_text = vim.api.nvim_get_current_line()
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
		end

		M.accepting = false
	end)

	return true
end

return M
