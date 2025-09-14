# emcc-sandboxd

## Installation

### Install emscripten

Install dependencies

```bash
sudo apt update
sudo apt install -y git cmake python3 build-essential
```

Clone emscripten

```bash
git clone https://github.com/emscripten-core/emsdk.git
cd emsdk
```

Install emscripten

```bash
./emsdk install latest
./emsdk activate latest
```

Setup emscripten environment

```bash
source ./emsdk_env.sh
echo 'source ~/emsdk/emsdk_env.sh' >> ~/.bashrc
```

### Install nsjail

Install dependencies

```bash
sudo apt update
sudo apt install -y git make g++ pkg-config libprotobuf-dev protobuf-compiler libnl-3-dev libnl-genl-3-dev libcap-dev libtool-bin libnl-route-3-dev flex bison
```

Clone nsjail & compile

```bash
git clone https://github.com/google/nsjail.git
cd nsjail
make -j$(nproc)
```

Install nsjail to system

```bash
sudo cp nsjail /usr/local/bin/
```

### Install go

Visit [https://go.dev/dl/](https://go.dev/dl/) to download the latest version of go.

For example, the package you downloaded is go1.25.1.linux-amd64.tar.gz, then run

```bash
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.1.linux-amd64.tar.gz
```

Setup go environment

```bash
echo -e '\nexport GOROOT=/usr/local/go\nexport GOPATH=$HOME/go\nexport PATH=$PATH:$GOROOT/bin:$GOPATH/bin' >> ~/.bashrc
source ~/.bashrc
```

### Run emcc-sandboxd

```bash
git clone https://github.com/elecmonkey/emcc-sandboxd
go run .
```