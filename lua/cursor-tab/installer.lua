local M = {}

function M.get_platform()
	-- Detect OS and architecture
	local os = vim.loop.os_uname().sysname:lower()
	local arch = vim.loop.os_uname().machine

	-- Map to release naming
	if os == "darwin" then
		if arch == "arm64" then
			return "darwin-arm64"
		else
			return "darwin-amd64"
		end
	elseif os == "linux" then
		if arch == "aarch64" or arch == "arm64" then
			return "linux-arm64"
		else
			return "linux-amd64"
		end
	end

	return nil -- Unsupported platform
end

function M.get_binary_path(plugin_dir)
	return plugin_dir .. "/bin/cursor-tab-server"
end

function M.binary_exists(binary_path)
	return vim.fn.filereadable(binary_path) == 1 and vim.fn.executable(binary_path) == 1
end

function M.download_binary(plugin_dir, callback)
	local platform = M.get_platform()
	if not platform then
		vim.notify("cursor-tab: Unsupported platform", vim.log.levels.ERROR)
		if callback then
			callback(false)
		end
		return false
	end

	local binary_name = "cursor-tab-server-" .. platform
	local url = string.format("https://github.com/bengu3/cursor-tab.nvim/releases/download/latest/%s", binary_name)

	local bin_dir = plugin_dir .. "/bin"
	local binary_path = bin_dir .. "/cursor-tab-server"
	local download_path = bin_dir .. "/" .. binary_name

	-- Create bin directory
	vim.fn.mkdir(bin_dir, "p")

	vim.notify("cursor-tab: Downloading server binary...", vim.log.levels.INFO)

	-- Download binary
	local download_job = vim.fn.jobstart({ "curl", "-fsSL", "-o", download_path, url }, {
		on_exit = function(_, exit_code)
			if exit_code ~= 0 then
				vim.notify("cursor-tab: Failed to download binary", vim.log.levels.ERROR)
				if callback then
					callback(false)
				end
				return
			end

			-- Make executable and rename
			vim.fn.system({ "chmod", "+x", download_path })
			vim.fn.system({ "mv", download_path, binary_path })

			vim.notify("cursor-tab: Binary installed successfully", vim.log.levels.INFO)
			if callback then
				callback(true)
			end
		end,
	})

	return download_job ~= 0 and download_job ~= -1
end

function M.ensure_binary(plugin_dir)
	local binary_path = M.get_binary_path(plugin_dir)

	-- Check if binary already exists and is executable
	if M.binary_exists(binary_path) then
		return true
	end

	-- Binary missing or not executable, try to download
	vim.notify("cursor-tab: Binary not found, downloading...", vim.log.levels.INFO)

	-- Download synchronously on first setup (blocking is acceptable here)
	local success = false
	M.download_binary(plugin_dir, function(result)
		success = result
	end)

	-- Wait for download to complete (with timeout)
	local timeout = 30000 -- 30 seconds
	local start = vim.loop.now()
	while not success and (vim.loop.now() - start) < timeout do
		vim.wait(100)
	end

	return success
end

return M
