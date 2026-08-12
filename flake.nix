{
  description = "Go + Datastar patient dashboard development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_26
              gopls
              gotools
              go-tools
              just
              mkcert
              nssTools
              docker-client
              docker-compose
              curl
              nodejs_24
              pnpm
              playwright-driver.browsers
              nixfmt
            ];

            PLAYWRIGHT_BROWSERS_PATH = "${pkgs.playwright-driver.browsers}";
            PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
            APP_SECRET = "local-development-only-secret-change-before-deploying";
            TLS_CERT_FILE = ".certs/localhost.pem";
            TLS_KEY_FILE = ".certs/localhost-key.pem";

            shellHook = ''
              echo "Go $(go version | awk '{print $3}') · run 'just' to list project commands"
            '';
          };
        }
      );
    };
}
