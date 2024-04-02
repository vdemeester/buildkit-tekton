{
  description = "buildkit-tekton nix package";

  outputs = { self, nixpkgs, ... }@inputs:
    let

      # Generate a user-friendly version number.
      version = builtins.substring 0 8 self.lastModifiedDate;

      # System types to support.
      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];

      # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Nixpkgs instantiated for supported system types.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });

    in
    {

      # Provide some binary packages for selected system types.
      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          buildkit-tekton = pkgs.buildGo120Module {
            pname = "buildkit-tekton";
            inherit version;
            # In 'nix develop', we don't need a copy of the source tree
            # in the Nix store.
            src = ./.;
            subPackages = [ "cmd/buildkit-tekton" ];

            # We use vendor, no need for vendorHash
            vendorHash = null;
          };
          tkn-local = pkgs.buildGo121Module {
            pname = "tkn-local";
            inherit version;
            # In 'nix develop', we don't need a copy of the source tree
            # in the Nix store.
            src = ./.;
            subPackages = [ "cmd/tkn-local" ];

            # We use vendor, no need for vendorHash
            vendorHash = null;
          };
          docker =
            let
              buildkit-tekton = self.defaultPackage.${system};
            in
            pkgs.dockerTools.buildLayeredImage {
              name = buildkit-tekton.pname;
              tag = buildkit-tekton.version;
              contents = [ buildkit-tekton ];

              config = {
                Cmd = [ "/bin/buildkit-tekton" ];
                WorkingDir = "/";
              };
            };
        });

      # The default package for 'nix build'. This makes sense if the
      # flake provides only one package or there is a clear "main"
      # package.
      defaultPackage = forAllSystems (system: self.packages.${system}.buildkit-tekton);

      githubActions = inputs.nix-github-actions.lib.mkGithubMatrix {
        checks = nixpkgs.lib.getAttrs [ "x86_64-linux" ] self.packages;
      };
    };

  # Nixpkgs / NixOS version to use.
  inputs.nixpkgs.url = "nixpkgs/nixos-23.05";
  inputs.nix-github-actions.url = "github:nix-community/nix-github-actions";
  inputs.nix-github-actions.inputs.nixpkgs.follows = "nixpkgs";
}
