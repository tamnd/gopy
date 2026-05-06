package initconfig

// ConfigParseCmdline walks c.Argv applying the short and long
// options CPython's getopt accepts and stamping the corresponding
// PyConfig fields. Unknown options return an exit-2 status, which
// matches CPython's "print usage, exit" behavior.
//
// On success c.RunCommand / c.RunModule / c.RunFilename and the
// option-derived fields (parser_debug, write_bytecode, optimization
// level, ...) are populated; warnoptions seen on the command line
// land in cmdlineWarnopts, the caller stitches them into
// c.WarnOptions in the right order.
//
// CPython: Python/initconfig.c:2865 config_parse_cmdline
func ConfigParseCmdline(c *PyConfig, cmdlineWarnopts *[]string) Status {
	g := newGetopt(c.Argv)
	printVersion := 0

	for {
		opt, _ := g.next()
		if opt == optEOF {
			break
		}

		if opt == 'c' {
			if c.RunCommand == "" {
				c.RunCommand = g.optArg + "\n"
			}
			break
		}
		if opt == 'm' {
			if c.RunModule == "" {
				c.RunModule = g.optArg
			}
			break
		}

		switch opt {
		case 0:
			// --check-hash-based-pycs
			switch g.optArg {
			case "always", "never", "default":
				// PyConfig holds the mode in CheckHashPycsMode in CPython.
				// gopy parks the value in a private field that the marshal
				// arm reads in v0.8.
				c.checkHashPycsMode = g.optArg
			default:
				return StatusExitCode(2)
			}
		case 1, 2, 3:
			// help-all / help-env / help-xoptions: not implemented in v0.7.
			return StatusExitCode(0)
		case 'b':
			c.BytesWarning++
		case 'd':
			c.ParserDebug++
		case 'i':
			c.Inspect++
			c.Interactive++
		case 'E', 'I', 'X':
			// -E / -I / -X handled by the pre-cmdline reader.
			// -X arguments still need to land in c.XOptions.
			if opt == 'X' {
				c.XOptions = append(c.XOptions, g.optArg)
			}
		case 'O':
			c.OptimizationLevel++
		case 'P':
			c.SafePath = 1
		case 'B':
			c.WriteBytecode = 0
		case 's':
			c.UserSiteDirectory = 0
		case 'S':
			c.SiteImport = 0
		case 't':
			// Reserved for backwards compatibility, ignored.
		case 'u':
			c.BufferedStdio = 0
		case 'v':
			c.Verbose++
		case 'x':
			c.SkipSourceFirstLine = 1
		case 'h', '?':
			return StatusExitCode(0)
		case 'V':
			printVersion++
		case 'W':
			if cmdlineWarnopts != nil {
				*cmdlineWarnopts = append(*cmdlineWarnopts, g.optArg)
			}
		case 'q':
			c.Quiet++
		case 'R':
			c.UseHashSeed = 0
		default:
			return StatusExitCode(2)
		}
	}

	if printVersion > 0 {
		return StatusExitCode(0)
	}

	if c.RunCommand == "" && c.RunModule == "" &&
		g.optInd < len(c.Argv) &&
		c.Argv[g.optInd] != "-" &&
		c.RunFilename == "" {
		c.RunFilename = c.Argv[g.optInd]
	}

	return StatusOk()
}
