# toolenv

A virtual tool environment management

## Installation

```bash
go install github.com/is-equal/toolenv@latest
```

## Configuration

An example of the `toolenv.yml`

```yaml
tools:
  - name: node
    version: 22.18.0
    url: "https://nodejs.org/dist/v{{.version}}/node-v{{.version}}-{{.os}}-{{.arch}}.tar.xz"
    env:
      PATH: "storage/node@{{.version}}/bin"
    normalization:
      arch:
        amd64: x64

  - name: go
    version: 1.25.0
    url: "https://golang.org/dl/go{{.version}}.{{.os}}-{{.arch}}.tar.gz"
    env:
      GOROOT: "storage/go@{{.version}}"
      PATH: "storage/go@{{.version}}/bin"
```

## Usage

To download the tools run:
```bash
toolenv
```

To activate the environment run:

```bash
source ./env/bin/activate
```