{
  description = "buildkit-tekton nix package";

  # Nixpkgs / NixOS version to use.
  inputs.nixpkgs.url = "nixpkgs/nixos-21.11";

  outputs = { self, nixpkgs }:
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
          buildkit-tekton = pkgs.buildGo117Module {
            pname = "buildkit-tekton";
            inherit version;
            # In 'nix develop', we don't need a copy of the source tree
            # in the Nix store.
            src = ./.;
            subPackages = [ "cmd/buildkit-tekton" ];

            # We use vendor, no need for vendorSha256
            vendorSha256 = null;
          };
          tkn-local-run = pkgs.buildGo117Module {
            pname = "buildkit-tekton";
            inherit version;
            # In 'nix develop', we don't need a copy of the source tree
            # in the Nix store.
            src = ./.;
            subPackages = [ "cmd/tkn-local-run" ];

            # We use vendor, no need for vendorSha256
            vendorSha256 = null;
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
    };
}
