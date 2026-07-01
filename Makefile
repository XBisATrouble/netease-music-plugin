# Plugin metadata
PLUGIN_NAME := netease

.PHONY: build package clean

# 编译为 WebAssembly(需要 TinyGo >= 0.34)
build:
	tinygo build -opt=2 -scheduler=none -no-debug -o plugin.wasm -target wasip1 -buildmode=c-shared .

# 打包为 .ndp(zip 归档)
package: build
	zip -j $(PLUGIN_NAME).ndp manifest.json plugin.wasm

clean:
	rm -f plugin.wasm $(PLUGIN_NAME).ndp
