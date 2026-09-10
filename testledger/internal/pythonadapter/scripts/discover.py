#!/usr/bin/env python3
"""Emit a deterministic NDJSON inventory of Python functions and methods."""

import argparse
import ast
import fnmatch
import hashlib
import json
from pathlib import Path


def digest(value):
    data = ast.dump(value, annotate_fields=True, include_attributes=False)
    return hashlib.sha256(data.encode("utf-8")).hexdigest()


def own_lines(node):
    lines = set()

    class Visitor(ast.NodeVisitor):
        def visit_FunctionDef(self, child):
            if child is node:
                self.generic_visit(child)

        def visit_AsyncFunctionDef(self, child):
            if child is node:
                self.generic_visit(child)

        def visit_ClassDef(self, child):
            if child is node:
                self.generic_visit(child)

        def generic_visit(self, child):
            line = getattr(child, "lineno", None)
            if line is not None and isinstance(child, (ast.stmt, ast.expr)):
                lines.add(line)
            super().generic_visit(child)

    Visitor().visit(node)
    # Everything above the first body statement executes at definition time,
    # outside any test context: the decorators, the `def` line, a signature
    # continued across lines, and any default or annotation expression on it.
    # None of it is evidence that a test called the function, so counting it
    # caps a short function below any useful threshold -- a one-line @property
    # could never exceed 50%.
    if node.body:
        first_body_line = node.body[0].lineno
        lines = {line for line in lines if line >= first_body_line}
    else:
        lines.discard(node.lineno)
    if node.body and isinstance(node.body[0], ast.Expr):
        value = node.body[0].value
        if isinstance(value, (ast.Str, ast.Constant)) and isinstance(getattr(value, "value", None), str):
            lines.discard(node.body[0].lineno)
    return sorted(lines)


class FunctionVisitor(ast.NodeVisitor):
    def __init__(self, relative_path):
        self.relative_path = relative_path
        self.stack = []
        self.class_depth = 0
        self.symbols = []

    def visit_ClassDef(self, node):
        self.stack.append(node.name)
        self.class_depth += 1
        self.generic_visit(node)
        self.class_depth -= 1
        self.stack.pop()

    def visit_FunctionDef(self, node):
        self._visit_function(node, "method" if self.class_depth else "function")

    def visit_AsyncFunctionDef(self, node):
        kind = "async_method" if self.class_depth else "async_function"
        self._visit_function(node, kind)

    def _visit_function(self, node, kind):
        qualified_name = ".".join(self.stack + [node.name])
        signature = ast.Module(
            body=[ast.FunctionDef(
                name=node.name,
                args=node.args,
                body=[ast.Pass()],
                decorator_list=node.decorator_list,
                returns=node.returns,
                type_comment=getattr(node, "type_comment", None),
            )],
            type_ignores=[],
        )
        body = ast.Module(body=node.body, type_ignores=[])
        self.symbols.append({
            "language": "python",
            "path": self.relative_path,
            "qualified_name": qualified_name,
            "kind": kind,
            "start_line": node.lineno,
            "end_line": getattr(node, "end_lineno", node.lineno),
            "executable_lines": own_lines(node),
            "semantic_hash": digest(node),
            "signature_hash": digest(signature),
            "body_hash": digest(body),
        })
        self.stack.append(node.name)
        self.generic_visit(node)
        self.stack.pop()


def matches(path, patterns):
    return any(fnmatch.fnmatch(path, pattern) for pattern in patterns)


def expand(root, patterns):
    paths = set()
    for pattern in patterns:
        paths.update(path for path in root.glob(pattern) if path.is_file())
    return paths


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--include", action="append", default=[])
    parser.add_argument("--exclude", action="append", default=[])
    args = parser.parse_args()

    root = Path(args.root).resolve()
    for path in sorted(expand(root, args.include)):
        relative = path.relative_to(root).as_posix()
        if matches(relative, args.exclude):
            continue
        try:
            source = path.read_text(encoding="utf-8")
            tree = ast.parse(source, filename=relative, type_comments=True)
        except (OSError, SyntaxError, UnicodeDecodeError) as exc:
            print(json.dumps({
                "type": "diagnostic",
                "severity": "error",
                "path": relative,
                "message": str(exc),
            }, sort_keys=True))
            continue
        visitor = FunctionVisitor(relative)
        visitor.visit(tree)
        for symbol in visitor.symbols:
            print(json.dumps({"type": "symbol", "symbol": symbol}, sort_keys=True))


if __name__ == "__main__":
    main()
