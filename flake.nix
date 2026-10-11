{
  description = "devShell for re-asmr-spider";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }: let
    # system should match the system you are running on
    system = "x86_64-linux";
  in {
    devShells."${system}".default = let
      pkgs = import nixpkgs { inherit system; };
    in
      pkgs.mkShellNoCC {
        packages = with pkgs; [
          go
          gopls
        ];

        GOPROXY = "https://goproxy.cn,direct";
        # 放到项目目录，避免污染全局 ~/go
        shellHook = ''
          export GOPATH="$PWD/.go"
          mkdir -p "$GOPATH"
        '';
      };
  };
}
