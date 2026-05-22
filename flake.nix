{
  description = "demo.datum.net — split-path prober, origin, and Datum platform binaries";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    # datumctl: standard flake, builds cleanly off its own flake.
    # Absolute path because relative `path:../datumctl` gets resolved
    # against the nix store, not the flake directory.
    datumctl = {
      url = "git+file:///home/gianarb/git/datum/datumctl";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };

    # ../app exposes a `cli-static` package (musl + static openssl) that
    # produces a `datum-connect` binary suitable for scp'ing to a vanilla
    # Debian box. We consume that directly instead of rebuilding here.
    app = {
      url = "git+file:///home/gianarb/git/datum/app";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs = { self, nixpkgs, flake-utils, datumctl, app, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfreePredicate = pkg:
            builtins.elem (pkgs.lib.getName pkg) [ "terraform" ];
        };

        # ------------------------------------------------------------------
        # Our demo binary (prober + origin)
        # ------------------------------------------------------------------
        demo = pkgs.buildGoModule {
          pname = "demo";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-THXY2JU+a6A/P9OGb6M2nqx3WqT/rdGmpcF1LVod8No=";
          env.CGO_ENABLED = "0";
          ldflags = [ "-s" "-w" ];
          subPackages = [ "cmd/demo" ];
          meta = {
            description = "Split-path prober and sample origin for demo.datum.net";
            mainProgram = "demo";
            license = pkgs.lib.licenses.mit;
          };
        };

        demo-linux-amd64 = demo.overrideAttrs (old: {
          env = (old.env or { }) // { GOOS = "linux"; GOARCH = "amd64"; };
        });
        demo-linux-arm64 = demo.overrideAttrs (old: {
          env = (old.env or { }) // { GOOS = "linux"; GOARCH = "arm64"; };
        });

      in
      {
        packages = {
          default = demo;

          demo              = demo;
          demo-linux-amd64  = demo-linux-amd64;
          demo-linux-arm64  = demo-linux-arm64;

          datum-connect = app.packages.${system}.cli-static;

          datumctl = datumctl.packages.${system}.default;
        };

        apps.default = {
          type = "app";
          program = "${demo}/bin/demo";
        };

        # nix run .#dev  — starts origin + prober via process-compose with
        # the right PATH baked in so demo and datumctl resolve without
        # entering the devShell first.
        apps.dev = {
          type = "app";
          program = toString (pkgs.writeShellScript "dev" ''
            export PATH="${demo}/bin:${datumctl.packages.${system}.default}/bin:$PATH"
            exec ${pkgs.process-compose}/bin/process-compose up "$@"
          '');
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            go-tools
            sqlite
            curl
            jq
            terraform
            process-compose
          ] ++ [
            demo
            datumctl.packages.${system}.default
          ];

          shellHook = ''
            echo "demo.datum.net devshell"
            echo "  go              $(go version | awk '{print $3}')"
            echo "  sqlite          $(sqlite3 --version | awk '{print $1}')"
            echo "  terraform       $(terraform version | head -1 | awk '{print $2}')"
            echo "  process-compose $(process-compose version | head -1)"
            echo ""
            echo "Local dev:"
            echo "  nix run .#dev               # starts origin + prober (process-compose, PATH pre-wired)"
            echo "  process-compose up          # same, if already in this shell"
            echo ""
            echo "Build targets:"
            echo "  nix build .#demo            # our prober + origin"
            echo "  nix build .#datum-connect   # CLI from ../app (cli-static)"
            echo "  nix build .#datumctl        # the Datum Cloud CLI"
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
