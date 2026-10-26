import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("prepare_public", Path(__file__).with_name("prepare-public-repository.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class PublicPreparationTest(unittest.TestCase):
    def fixture(self, root):
        (root / "cmd").mkdir()
        (root / "cmd/main.go").write_text("package main\n")
        (root / "README.md").write_text("Public introduction\n")
        (root / "AGENTS.md").write_text("Portable agent instructions\n")
        (root / "notes.md").write_text("Private planning\n")
        (root / ".git").mkdir()
        (root / ".git/config").write_text("private history\n")
        return dict(schema_version=1, source_roots=["cmd/"], files=["AGENTS.md", "README.md"])

    def test_explicit_tracked_selection_excludes_history_and_notes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = self.fixture(root)
            rows = m.selection(root, config, ["AGENTS.md", "cmd/main.go", "README.md", "notes.md"])
            self.assertEqual([v["path"] for v in rows], ["AGENTS.md", "README.md", "cmd/main.go"])
            out = root / "new-public"
            m.materialize(root, out, rows)
            self.assertFalse((out / ".git").exists())
            self.assertFalse((out / "notes.md").exists())
            self.assertEqual((out / "AGENTS.md").read_bytes(), (root / "AGENTS.md").read_bytes())
            self.assertEqual((out / "cmd/main.go").read_bytes(), (root / "cmd/main.go").read_bytes())
            with self.assertRaises(FileExistsError):
                m.materialize(root, out, rows)

    def test_missing_tracked_file_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = self.fixture(root)
            with self.assertRaisesRegex(ValueError, "must be tracked"):
                m.selection(root, config, ["cmd/main.go"])

    def test_traversal_and_runtime_files_fail(self):
        for bad in ["../outside", ".git/config", "C:/secret", "cmd/../secret"]:
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                m.safe_path(bad)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = self.fixture(root)
            (root / "cmd/secrets.yaml").write_text("not a public fixture")
            with self.assertRaisesRegex(ValueError, "Runtime/data"):
                m.selection(root, config, ["AGENTS.md", "cmd/main.go", "README.md", "cmd/secrets.yaml"])

    def test_drift_aborts_without_final_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = self.fixture(root)
            rows = m.selection(root, config, ["AGENTS.md", "cmd/main.go", "README.md"])
            (root / "cmd/main.go").write_text("changed after plan")
            out = root / "public"
            with self.assertRaisesRegex(ValueError, "Input changed"):
                m.materialize(root, out, rows)
            self.assertFalse(out.exists())
            self.assertFalse(list(root.glob(".public-*")))


if __name__ == "__main__":
    unittest.main()
