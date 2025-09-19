A rsync + git + ssh tool for quick remote development

## Install

```sh
git clone https://github.com/littledivy/buildon
make buildon
sudo make install
```

## Usage

```toml
[remote.windows]
host = "192.168.0.1"
user = "divy"
shell = "powershell"
path = "Projects/deno"
```

```sh
$ buildon windows cargo b
```
