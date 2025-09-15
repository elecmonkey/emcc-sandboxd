# emcc-sandboxd

About
Sandboxed Emscripten compilation http server with resource limits and safety measures.

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

## Test

Health check

```bash
curl http://localhost:8080/healthz
```

Compile C code

```bash
curl -X POST http://localhost:8080/compile \ \
  -H "Content-Type: application/json" \
  -d '{
    "code": "#include <stdio.h>\nint main() { printf(\"Hello WebAssembly!\"); return 0; }",
    "type": "c"
  }'
```

Compile C++ code

```bash
curl -X POST http://localhost:8080/compile \
  -H "Content-Type: application/json" \
  -d '{
    "code": "#include <iostream>\nint main() { std::cout << \"Hello C++ WebAssembly!\" << std::endl; return 0; }",
    "type": "cpp"
  }'
```

Compile with custom arguments

```bash
curl -X POST http://localhost:8080/compile \
  -H "Content-Type: application/json" \
  -d '{
    "code": "#include <stdio.h>\nint add(int a, int b) { return a + b; }\nint main() { printf(\"Result: %d\", add(5, 3)); return 0; }",
    "type": "c",
    "args": ["-O2", "-sEXPORTED_FUNCTIONS=[_main,_add]"]
  }'
```

## Configuration

emcc-sandboxd supports configuration through a JSON file named `config.json`. If no configuration file is provided, the service will use built-in default values.

### Configuration File Structure

```json
{
  "workingDir": "/srv/emcc-sandboxd",
  "addr": ":8080",
  "baseDir": ".",
  "jobsDir": "jobs",
  "artifactsDir": "artifacts",
  "enableStaticArtifacts": true,
  "artifactTTLDays": 3,
  "cleanupIntervalMins": 30,
  "defaultArgs": [
    "-sINVOKE_RUN=0",
    "-sENVIRONMENT=web",
    "-sALLOW_MEMORY_GROWTH=1",
    "-sMODULARIZE=1"
  ],
  "nsjailEnabled": false,
  "nsjailPath": "nsjail",
  "cgroupV2Root": "cgroup",
  "enableResourceGating": false,
  "jobMemoryEstimateMB": 256
}
```

### Configuration Options

#### Basic Settings

- **`workingDir`** (string): The root working directory for the service. Default: `/srv/emcc-sandboxd`
  - If empty, uses the current working directory
  - All relative paths are resolved from this directory

- **`addr`** (string): HTTP server listening address. Default: `:8080`
  - Format: `[host]:port` (e.g., `:8080`, `localhost:3000`, `0.0.0.0:8080`)

- **`baseDir`** (string): Base directory for internal file operations. Default: `.`
  - Used as the root for `jobsDir` and `artifactsDir`

#### Directory Structure

- **`jobsDir`** (string): Directory name for temporary compilation workspaces. Default: `jobs`
  - Each compilation job gets a subdirectory `jobs/<jobid>/`
  - Contains source files and intermediate build artifact and automatically cleaned up after compilation

- **`artifactsDir`** (string): Directory name for final compilation artifacts. Default: `artifacts`
  - Final WebAssembly files are stored in `artifacts/<jobid>/`
  - Contains `app.js` and `app.wasm` files
  - Served via HTTP static file service

#### Static File Service

- **`enableStaticArtifacts`** (boolean): Enable HTTP static file serving for artifacts. Default: `true`
  - When enabled, artifacts are accessible via GET requests
  - URLs format: `/artifacts/<jobid>/app.js` and `/artifacts/<jobid>/app.wasm`
  - Can be cached by CDN or reverse proxy

#### Cleanup Management

- **`artifactTTLDays`** (integer): Time-to-live for artifacts in days. Default: `3`
  - Artifacts older than this will be automatically deleted
  - Set to `0` to disable automatic cleanup

- **`cleanupIntervalMins`** (integer): Cleanup check interval in minutes. Default: `30`
  - How often the cleanup process runs
  - Lower values = more frequent cleanup, higher overhead

#### Compilation Settings

- **`defaultArgs`** (array of strings): Default Emscripten compilation arguments
  - Applied to all compilation requests
  - User-provided arguments are merged with these defaults
  - Common defaults:
    - `-sINVOKE_RUN=0`: Don't automatically call main()
    - `-sENVIRONMENT=web`: Target web browsers
    - `-sALLOW_MEMORY_GROWTH=1`: Allow runtime memory expansion
    - `-sMODULARIZE=1`: Generate modular JavaScript output

#### Security and Sandboxing

- **`nsjailEnabled`** (boolean): Enable nsjail sandboxing. Default: `false`
  - **Recommended for production**: `true`
  - Isolates compilation process from host system
  - Requires nsjail to be installed on the system

- **`nsjailPath`** (string): Path to nsjail executable. Default: `nsjail`
  - Can be absolute path (e.g., `/usr/local/bin/nsjail`) or command name
  - Only used when `nsjailEnabled` is `true`

#### Resource Management

- **`enableResourceGating`** (boolean): Enable memory-based resource gating. Default: `false`
  - **Recommended for production**: `true`
  - Prevents system overload by gating new compilations against a global memory budget, not by a fixed worker count
  - Uses cgroups v2 memory limits for enforcement (reads `memory.max` and `memory.current` from the configured cgroup)

- **`jobMemoryEstimateMB`** (integer): Estimated memory usage per compilation job in MB. Default: `256`
  - Used only for total-pool gating calculations; it is not a per-job hard memory cap
  - Effective concurrency emerges from `floor((memory.max - memory.current) / estimate)` at runtime
  - Choose a conservative value to avoid oversubscription; increase if your jobs are lightweight

- **`cgroupV2Root`** (string): Root directory for cgroups v2 operations. Default: `cgroup`
  - Only used when `enableResourceGating` is `true`
  - Should point to a valid cgroups v2 mount point
  - Common production path: `/sys/fs/cgroup/emcc-sandboxd`

#### How resource gating works

- When `enableResourceGating=true`, each incoming `/compile` request attempts to reserve `jobMemoryEstimateMB` from a shared budget before starting the compiler.
- The server reads `memory.max` (global cap) and `memory.current` (live usage) from `cgroupV2Root`. A job proceeds only if `current + reserved + estimate <= max`; otherwise it waits and retries every ~200ms.
- Reservations are tracked locally in the server to avoid races across concurrent HTTP requests; on completion the reservation is released.
- If `memory.max` is `"max"` or the files cannot be read, gating is effectively disabled (requests proceed immediately).
- Requests respect HTTP cancellation/timeout; if the client disconnects or the context expires while waiting, the request aborts.

#### What resource gating is not

- It does not impose a per-job hard memory limit. All compiler processes inherit the same cgroup as the service.
- A single job that exceeds the estimate can still push the cgroup toward OOM. To hard-cap per-job resources, place each job in its own cgroup or configure stricter nsjail limits.

#### Enforcing per-job limits (optional)

- To enforce per-job memory/CPU/PID caps, consider one of:
  - Launch each job in its own cgroup and write per-job `memory.max`, `cpu.max`, and `pids.max`.
  - Use nsjail with appropriate rlimits and cgroup integration. Note: the current default only sets `--rlimit_fsize` (256MiB) and disables networking; it does not set per-job memory/CPU caps.
