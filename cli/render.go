package cli

import (
	"io/fs"
	"os"
	"path"
	"strings"

	v1 "github.com/hashicorp/nomad-openapi/v1"
	"github.com/hashicorp/nomad-pack/flag"
	"github.com/hashicorp/nomad-pack/internal/pkg/cache"
	"github.com/hashicorp/nomad-pack/internal/pkg/errors"
	"github.com/hashicorp/nomad-pack/internal/pkg/helper/filesystem"
	"github.com/hashicorp/nomad-pack/terminal"
	"github.com/posener/complete"
)

// RenderCommand is a command that allows users to render the templates within
// a pack and display them on the console. This is useful when developing or
// debugging packs.
type RenderCommand struct {
	*baseCommand
	packConfig *cache.PackConfig
	// renderOutputTemplate is a boolean flag to control whether the output
	// template is rendered.
	renderOutputTemplate bool
	// renderToDir is the path to write rendered job files to in addition to
	// standard output.
	renderToDir string
}

// Run satisfies the Run function of the cli.Command interface.
func (c *RenderCommand) Run(args []string) int {
	c.cmdKey = "render" // Add cmdKey here to print out helpUsageMessage on Init error

	if err := c.Init(
		WithExactArgs(1, args),
		WithFlags(c.Flags()),
		WithNoConfig()); err != nil {

		c.ui.ErrorWithContext(err, ErrParsingArgsOrFlags)
		c.ui.Info(c.helpUsageMessage())

		return 1
	}

	c.packConfig.Name = c.args[0]

	// Set the packConfig defaults if necessary and generate our UI error context.
	errorContext := initPackCommand(c.packConfig)

	if err := cache.VerifyPackExists(c.packConfig, errorContext, c.ui); err != nil {
		return 1
	}

	client, err := v1.NewClient()
	if err != nil {
		c.ui.ErrorWithContext(err, "failed to initialize client", errorContext.GetAll()...)
		return 1
	}

	// fast failure start around aiming to-dir at a file rather than a directory
	if filesystem.Exists(c.packConfig.Path, true) && !filesystem.IsDir(c.packConfig.Path, true) {
		err = errors.New("output path exists and is not a directory")
		c.ui.ErrorWithContext(err, "failed to create output directory", errorContext.GetAll()...)
		return 1
	}
	packManager := generatePackManager(c.baseCommand, client, c.packConfig)
	renderOutput, err := renderPack(packManager, c.baseCommand.ui, errorContext)
	if err != nil {
		return 1
	}

	// The render command should at least render one parent, or one dependant
	// pack template.
	if renderOutput.LenParentRenders() < 1 && renderOutput.LenDependentRenders() < 1 {
		c.ui.ErrorWithContext(errors.ErrNoTemplatesRendered, "no templates rendered", errorContext.GetAll()...)
		return 1
	}

	var renders = []Render{}

	// Iterate the rendered files and add these to the list of renders to
	// output. This allows errors to surface and end things without emitting
	// partial output and then erroring out.

	for name, renderedFile := range renderOutput.DependentRenders() {
		renders = append(renders, Render{Name: formatRenderName(name), Content: renderedFile, c: c, ec: errorContext})
	}
	for name, renderedFile := range renderOutput.ParentRenders() {
		renders = append(renders, Render{Name: formatRenderName(name), Content: renderedFile, c: c, ec: errorContext})
	}

	// If the user wants to render and display the outputs template file then
	// render this. In the event the render returns an error, print this but do
	// not exit. The render can fail due to template function errors, but we
	// can still display the pack templates from above. The error will be
	// displayed before the template renders, so the UI looks OK.
	if c.renderOutputTemplate {
		outputRender, err := packManager.ProcessOutputTemplate()
		if err != nil {
			c.ui.ErrorWithContext(err, "failed to render output template", errorContext.GetAll()...)
		} else {
			renders = append(renders, Render{Name: "outputs.tpl", Content: outputRender, c: c, ec: errorContext})
		}
	}

	// Output the renders. Output the files first if enabled so that any renders
	// that display will also have been written to disk.
	for _, render := range renders {
		if err, ec := render.Output(); err != nil {
			c.ui.ErrorWithContext(err, "error rendering to file", ec.GetAll()...)
		}
	}

	return 0
}

func (c *RenderCommand) Flags() *flag.Sets {
	return c.flagSet(flagSetOperation, func(set *flag.Sets) {
		c.packConfig = &cache.PackConfig{}

		f := set.NewSet("Render Options")

		f.StringVar(&flag.StringVar{
			Name:    "registry",
			Target:  &c.packConfig.Registry,
			Default: "",
			Usage: `Specific registry name containing the pack to be rendered.
If not specified, the default registry will be used.`,
		})

		f.StringVar(&flag.StringVar{
			Name:    "ref",
			Target:  &c.packConfig.Ref,
			Default: "",
			Usage: `Specific git ref of the pack to be rendered. 
Supports tags, SHA, and latest. If no ref is specified, defaults to latest.

Using ref with a file path is not supported.`,
		})

		f.BoolVar(&flag.BoolVar{
			Name:    "render-output-template",
			Target:  &c.renderOutputTemplate,
			Default: false,
			Usage: `Controls whether or not the output template file within the
                      pack is rendered and displayed.`,
		})

		f.StringVarP(&flag.StringVarP{
			StringVar: &flag.StringVar{
				Name:   "to-dir",
				Target: &c.renderToDir,
				Usage: `Path to write rendered job files to in addition to standard
				output.`,
				// Aliases: []string{"to"},
			},
			Shorthand: "o",
		})

	})
}

func (c *RenderCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *RenderCommand) AutocompleteFlags() complete.Flags {
	return c.Flags().Completions()
}

// Help satisfies the Help function of the cli.Command interface.
func (c *RenderCommand) Help() string {

	c.Example = `
	# Render an example pack with override variables in a variable file.
	nomad-pack render example --var-file="./overrides.hcl"

	# Render an example pack with cli variable overrides.
	nomad-pack render example --var="redis_image_version=latest" \
		--var="redis_resources={"cpu": "1000", "memory": "512"}"

	# Render an example pack including the outputs template file.
	nomad-pack render example --render-output-template

	# Render an example pack, outputting the rendered templates to file in
	# addition to the terminal. Setting auto-approve allows the command to
	# overwrite existing files.
	nomad-pack render example --to-dir ~/out --auto-approve

    # Render a pack under development from the filesystem - supports current working 
    # directory or relative path
	nomad-pack render . 
	`

	return formatHelp(`
	Usage: nomad-pack render <pack-name> [options]

	Render the specified Nomad Pack and view the results.

` + c.GetExample() + c.Flags().Help())
}

// Synopsis satisfies the Synopsis function of the cli.Command interface.
func (c *RenderCommand) Synopsis() string {
	return "Render the templates within a pack"
}

type Render struct {
	Name    string
	Content string
	c       *RenderCommand
	ec      *errors.UIErrorContext
}

// Output is the primary method for emitting the rendered templates to their
// destinations.
func (r Render) Output() (err error, ec *errors.UIErrorContext) {
	if r.c.renderToDir != "" {
		err = r.toFile()
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return err, r.ec
			}
		}
	}

	r.toTerminal()
	return nil, r.ec
}

func (r Render) toTerminal() {
	r.c.ui.Output(r.Name+":", terminal.WithStyle(terminal.BoldStyle))
	r.c.ui.Output("")
	r.c.ui.Output(r.Content)
}

func (r Render) toFile() (err error) {
	filePath, fileName := path.Split(r.Name)
	outDir := path.Join(r.c.renderToDir, filePath)

	filesystem.CreatePath(outDir, false)
	outFile := path.Join(outDir, fileName)

	overwrite, err := maybeConfirmOverwrite(outFile, r.c)
	if err != nil {
		// the caller should check to see if the error is a context.Canceled error
		// which signals a keyboard interrupt in the confirmation loop.
		return err
	}
	if !overwrite {
		return fs.ErrExist
	}
	err = filesystem.WriteFile(outFile, r.Content, r.c.autoApproved || overwrite)
	if err != nil {
		r.ec.Add(errors.RenderContextDestFile, outFile)
		return err
	}

	return nil
}

// confirmOverwrite prompts the user to confirm that they want to overwrite. If
// the auto-approved flag is set, the function always returns true. If the
// command is running non-interactively, it will return false. Otherwise, it will
// loop on invalid input until the user chooses `y` or `n`.
func maybeConfirmOverwrite(path string, c *RenderCommand) (bool, error) {
	// if the flag is set, we don't need to prompt the user
	if c.autoApproved {
		return true, nil
	}

	// there's nothing to ask about if a file doesn't exist at the destination
	if !filesystem.Exists(path, false) {
		return true, nil
	}
	// if the ui is not interactive, we should return false.
	if !c.ui.Interactive() {
		return false, nil
	}

	// loop until we get a valid response
	for {
		overwrite, err := c.ui.Input(&terminal.Input{
			Prompt: "Output file exists, overwrite? [y/n/a] ",
			Style:  terminal.WarningBoldStyle,
		})
		if err != nil {
			return false, err
		}
		overwrite = strings.ToLower(overwrite)
		if overwrite == "y" || overwrite == "n" || overwrite == "a" {
			if overwrite == "a" {
				c.autoApproved = true
			}
			return overwrite == "y", nil
		}
	}
}

// formatRenderName trims the low-value elements from the rendered template
// name.
func formatRenderName(name string) string {
	outName := strings.Replace(name, "/templates/", "/", 1)
	outName = strings.TrimRight(outName, ".tpl")

	return outName
}
