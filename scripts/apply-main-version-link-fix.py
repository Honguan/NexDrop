from pathlib import Path

main_path = Path("cmd/nexdrop/main.go")
main = main_path.read_text()
old_declaration = "var version = buildversion.ProductVersion"
new_declaration = 'var version = "development"'
old_import = '\tbuildversion "nexdrop/internal/version"\n'
if main.count(old_declaration) != 1:
    raise SystemExit(f"expected one main version declaration, found {main.count(old_declaration)}")
if main.count(old_import) != 1:
    raise SystemExit(f"expected one buildversion import, found {main.count(old_import)}")
main = main.replace(old_declaration, new_declaration, 1)
main = main.replace(old_import, "", 1)
main_path.write_text(main)
