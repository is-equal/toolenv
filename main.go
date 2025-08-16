package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

const OS_PERMISSION os.FileMode = 0755

type Config struct {
	Tools []Tool `yaml:"tools"`
}

type Tool struct {
	Name          string             `yaml:"name"`
	Version       string             `yaml:"version"`
	URL           string             `yaml:"url"`
	Env           map[string]string  `yaml:"env,omitempty"`
	Normalization *ToolNormalization `yaml:"normalization,omitempty"`
}

type ToolNormalization struct {
	Arch map[string]string `yaml:"arch"`
	OS   map[string]string `yaml:"os"`
}

func main() {
	app := &cli.Command{
		Name:  "toolenv",
		Usage: "A virtual tool environment manager",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return setup("env")
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func setup(env_name string) error {
	config, err := loadEnv()

	if err != nil {
		return err
	}

	env_dir := filepath.Join(".", env_name)
	bin_dir := filepath.Join(env_dir, "bin")

	if err := os.MkdirAll(bin_dir, OS_PERMISSION); err != nil {
		return fmt.Errorf("failed to create env: %w", err)
	}

	storage_dir := filepath.Join(env_name, "storage")

	if err := os.RemoveAll(storage_dir); err != nil {
		return fmt.Errorf("failed to cleanup the storage: %w", err)
	}

	for _, tool := range config.Tools {
		os_name, arch := normalizeSystem(tool.Normalization)

		url, err := buildURL(tool.URL, tool.Version, os_name, arch)

		if err != nil {
			return fmt.Errorf("failed to build the tool URL: %w", err)
		}

		install_dir := filepath.Join(storage_dir, fmt.Sprintf("%s@%s", tool.Name, tool.Version))

		if err := os.MkdirAll(install_dir, OS_PERMISSION); err != nil {
			return fmt.Errorf("failed to create installation directory: %w", err)
		}

		fmt.Printf("\nInstalling %s version %s ...\n", tool.Name, tool.Version)

		if err := installTool(url, install_dir); err != nil {
			return fmt.Errorf("failed to download and extract %s: %w", tool.Name, err)
		}

		fmt.Printf("|- Done!\n\n")
	}

	if err := generateActivationScript(env_name, config.Tools); err != nil {
		return err
	}

	fmt.Printf("Environment created at ./%s\n", env_dir)
	fmt.Printf("Activate with: source ./%s/bin/activate\n", env_dir)

	return nil
}

func loadEnv() (*Config, error) {
	f, err := os.Open("toolenv.yml")

	if err != nil {
		return nil, fmt.Errorf("failed to open toolenv.yml: %w", err)
	}
	defer f.Close()

	var data Config

	if err := yaml.NewDecoder(f).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to parse toolenv.yml: %w", err)
	}

	return &data, nil
}

func normalizeSystem(spec *ToolNormalization) (string, string) {
	arch := runtime.GOARCH
	os_name := runtime.GOOS

	if spec != nil {
		if mapped_arch, ok := spec.Arch[arch]; ok {
			arch = mapped_arch
		}

		if mapped_os, ok := spec.OS[os_name]; ok {
			os_name = mapped_os
		}
	}

	return os_name, arch
}

func buildURL(template_string, version, os_name, arch string) (string, error) {
	tmpl, err := template.New("url").Parse(template_string)

	if err != nil {
		return "", err
	}

	data := map[string]string{
		"version": version,
		"os":      os_name,
		"arch":    arch,
	}

	var buf bytes.Buffer

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func installTool(url, install_dir string) error {
	fmt.Printf("|- Downloading from \"%s\"\n", url)

	response, err := http.Get(url)

	if err != nil {
		return err
	}

	defer response.Body.Close()

	if response.StatusCode != 200 {
		return fmt.Errorf("failed to download file: %s", response.Status)
	}

	tmp_file, err := os.CreateTemp("", "toolenv-download-*")

	if err != nil {
		return err
	}

	fmt.Printf("|- Creating a temporary file %s\n", tmp_file.Name())

	defer fmt.Printf("|- Removing the temporary file %s\n", tmp_file.Name())
	defer os.Remove(tmp_file.Name())
	defer tmp_file.Close()

	if _, err := io.Copy(tmp_file, response.Body); err != nil {
		return err
	}

	extension := filepath.Ext(url)

	switch extension {
	case ".gz", ".tgz":
		return extractTarGz(tmp_file.Name(), install_dir)
	case ".xz":
		return extractTarXz(tmp_file.Name(), install_dir)
	default:
		return fmt.Errorf("unsupported archive format: %s", extension)
	}
}

func extractTarGz(filename, install_dir string) error {
	cmd := exec.Command("tar", "-xzf", filename, "-C", install_dir, "--strip-components=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func extractTarXz(filename, install_dir string) error {
	cmd := exec.Command("tar", "-xJf", filename, "-C", install_dir, "--strip-components=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func generateActivationScript(env_name string, tools []Tool) error {
	activate_dir := filepath.Join(env_name, "bin", "activate")

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# This file must be used with \"source %s/bin/activate\"\n", env_name))
	sb.WriteString("# It modifies the current shell environment.\n")
	sb.WriteString(fmt.Sprintf("export TOOLENV_DIR=\"$(pwd)/%s\"\n", env_name))
	sb.WriteString("export PREVIOUS_PATH=\"$PATH\"\n")

	for _, tool := range tools {
		for key, value := range tool.Env {
			escaped_value := strings.ReplaceAll(value, `"`, `\"`)

			tmpl, err := template.New("env").Parse(escaped_value)

			if err != nil {
				return err
			}

			data := map[string]string{
				"version": tool.Version,
			}

			var buf bytes.Buffer

			if err := tmpl.Execute(&buf, data); err != nil {
				return err
			}

			if key == "PATH" {
				sb.WriteString(fmt.Sprintf("export PATH=\"$TOOLENV_DIR/%s:$PATH\"\n", buf.String()))
			} else {
				sb.WriteString(fmt.Sprintf("export %s=\"$TOOLENV_DIR/%s\"\n", key, buf.String()))
			}
		}
	}

	sb.WriteString("export OLD_PS1=\"$PS1\"\n")
	sb.WriteString("export PS1=\"(toolenv:env) $PS1\"\n")
	sb.WriteString("deactivate() {\n")
	sb.WriteString("\texport PS1=\"$OLD_PS1\"\n")
	sb.WriteString("\tunset OLD_PS1\n")

	for _, tool := range tools {
		for key := range tool.Env {
			if key == "PATH" {
				continue
			}

			sb.WriteString(fmt.Sprintf("\tunset %s\n", key))
		}
	}

	sb.WriteString("\texport PATH=$PREVIOUS_PATH\n")
	sb.WriteString("\tunset PREVIOUS_PATH\n")
	sb.WriteString("\tunset TOOLENV_DIR\n")
	sb.WriteString("\tunset -f deactivate\n")
	sb.WriteString("}")

	return os.WriteFile(activate_dir, []byte(sb.String()), OS_PERMISSION)
}
