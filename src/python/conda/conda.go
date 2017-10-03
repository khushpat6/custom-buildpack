package conda

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudfoundry/libbuildpack"
	"github.com/kr/text"
)

type Stager interface {
	BuildDir() string
	CacheDir() string
	DepDir() string
	DepsIdx() string
	LinkDirectoryInDepDir(string, string) error
	WriteProfileD(string, string) error
}

type Manifest interface {
	InstallOnlyVersion(string, string) error
}

type Command interface {
	Execute(string, io.Writer, io.Writer, string, ...string) error
}

type Conda struct {
	Manifest Manifest
	Stager   Stager
	Command  Command
	Log      *libbuildpack.Logger
}

func New(m Manifest, s Stager, c Command, l *libbuildpack.Logger) *Conda {
	return &Conda{
		Manifest: m,
		Stager:   s,
		Command:  c,
		Log:      l,
	}
}

func Run(c *Conda) error {
	if err := c.Install(c.Version()); err != nil {
		c.Log.Error("Could not install conda: %v", err)
		return err
	}

	// FIXME Conda cache -- https://github.com/cloudfoundry/python-buildpack/blob/master/bin/steps/conda-supply#L64-L72

	if err := c.UpdateAndClean(); err != nil {
		c.Log.Error("Could not update conda env: %v", err)
		return err
	}

	// FIXME Conda cache -- https://github.com/cloudfoundry/python-buildpack/blob/master/bin/steps/conda-supply#L81-L83

	c.Stager.LinkDirectoryInDepDir(filepath.Join(c.Stager.DepDir(), "conda", "bin"), "bin")
	if err := c.Stager.WriteProfileD("conda.sh", c.ProfileD()); err != nil {
		c.Log.Error("Could not write profile.d script: %v", err)
		return err
	}

	c.Log.BeginStep("Done")
	return nil
}

func (c *Conda) Version() string {
	if runtime, err := ioutil.ReadFile(filepath.Join(c.Stager.BuildDir(), "runtime.txt")); err == nil {
		if strings.HasPrefix(string(runtime), "python-3") {
			return "miniconda3"
		}
	}
	return "miniconda2"
}

func (c *Conda) Install(version string) error {
	c.Log.BeginStep("Supplying conda")
	var installer string
	if f, err := ioutil.TempFile("", "miniconda.sh"); err != nil {
		return err
	} else {
		f.Close()
		installer = f.Name()
		defer os.Remove(installer)
	}

	if err := c.Manifest.InstallOnlyVersion(version, installer); err != nil {
		return fmt.Errorf("Error downloading miniconda: %v", err)
	}
	if err := os.Chmod(installer, 0755); err != nil {
		return err
	}

	c.Log.BeginStep("Installing Miniconda")
	condaHome := filepath.Join(c.Stager.DepDir(), "conda")
	if err := c.Command.Execute("/", indentWriter(os.Stdout), indentWriter(os.Stderr), installer, "-b", "-p", condaHome); err != nil {
		return fmt.Errorf("Error installing miniconda: %v", err)
	}

	return nil
}

func (c *Conda) UpdateAndClean() error {
	c.Log.BeginStep("Installing Dependencies")
	c.Log.BeginStep("Installing conda environment from environment.yml")

	condaHome := filepath.Join(c.Stager.DepDir(), "conda")
	if err := c.Command.Execute("/", indentWriter(os.Stdout), indentWriter(os.Stderr), filepath.Join(condaHome, "bin", "conda"), "env", "update", "--quiet", "-n", "dep_env", "-f", filepath.Join(c.Stager.BuildDir(), "environment.yml")); err != nil {
		return fmt.Errorf("Could not run conda env update: %v", err)
	}
	if err := c.Command.Execute("/", indentWriter(os.Stdout), indentWriter(os.Stderr), filepath.Join(condaHome, "bin", "conda"), "clean", "-pt"); err != nil {
		c.Log.Error("Could not run conda clean: %v", err)
		return fmt.Errorf("Could not run conda clean: %v", err)
	}

	return nil
}

func (c *Conda) ProfileD() string {
	return fmt.Sprintf(`grep -rlI %s $DEPS_DIR/%s/conda | xargs sed -i -e "s|%s|$DEPS_DIR/%s|g"
source activate dep_env
`, c.Stager.DepDir(), c.Stager.DepsIdx(), c.Stager.DepDir(), c.Stager.DepsIdx())
}

func indentWriter(writer io.Writer) io.Writer {
	return text.NewIndentWriter(writer, []byte("       "))
}
