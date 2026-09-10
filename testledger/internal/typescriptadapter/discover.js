const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const searchRoot = path.resolve(input.root, input.working_directory || '.');
let ts;
try { ts = require(require.resolve('typescript', {paths: [searchRoot, input.root]})); }
catch (error) { process.stderr.write('TypeScript compiler API is required in the configured project: ' + error.message); process.exit(2); }
const printer = ts.createPrinter({removeComments: true, newLine: ts.NewLineKind.LineFeed});
const digest = value => crypto.createHash('sha256').update(value).digest('hex');
const results = [];
const functionLike = node => ts.isFunctionDeclaration(node) || ts.isMethodDeclaration(node) || ts.isGetAccessor(node) || ts.isSetAccessor(node) || ts.isConstructorDeclaration(node);
const expressionFunction = node => ts.isArrowFunction(node) || ts.isFunctionExpression(node);
for (const relative of input.files) {
  const absolute = path.join(input.root, relative);
  const source = ts.createSourceFile(relative, fs.readFileSync(absolute, 'utf8'), ts.ScriptTarget.Latest, true, relative.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const visit = (node, scope) => {
    let nextScope = scope;
    if (ts.isClassDeclaration(node) && node.name) nextScope = scope.concat(node.name.text);
    if (ts.isVariableDeclaration(node) && node.name && ts.isObjectLiteralExpression(node.initializer)) nextScope = scope.concat(node.name.getText(source));
    if (functionLike(node) && node.body) {
      const own = ts.isConstructorDeclaration(node) ? 'constructor' : (node.name && node.name.getText(source));
      if (own) add(node, nextScope.concat(own).join('.'), ts.isMethodDeclaration(node) || ts.isConstructorDeclaration(node) || ts.isAccessor(node) ? 'method' : 'function');
      return;
    }
    if (ts.isVariableDeclaration(node) && node.name && expressionFunction(node.initializer)) {
      add(node.initializer, scope.concat(node.name.getText(source)).join('.'), 'function');
      return;
    }
    if ((ts.isPropertyDeclaration(node) || ts.isPropertyAssignment(node)) && node.name && expressionFunction(node.initializer)) {
      add(node.initializer, scope.concat(node.name.getText(source)).join('.'), 'method');
      return;
    }
    ts.forEachChild(node, child => visit(child, nextScope));
  };
  const add = (node, qualified_name, kind) => {
    const start = source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
    const end = source.getLineAndCharacterOfPosition(node.end).line + 1;
    const body = node.body ? printer.printNode(ts.EmitHint.Unspecified, node.body, source) : '';
    const modifiers = node.modifiers ? node.modifiers.map(m => m.getText(source)).join(' ') : '';
    const typeParameters = node.typeParameters ? node.typeParameters.map(p => p.getText(source)).join(',') : '';
    const signature = modifiers + '|' + typeParameters + '|' + (node.parameters ? node.parameters.map(p => printer.printNode(ts.EmitHint.Unspecified, p, source)).join(',') : '') + ':' + (node.type ? node.type.getText(source) : '');
    const canonical = printer.printNode(ts.EmitHint.Unspecified, node, source);
    const lines = new Set();
    const statements = current => {
      if (current !== node && (functionLike(current) || expressionFunction(current))) return;
      if (ts.isStatement(current) && !ts.isBlock(current)) lines.add(source.getLineAndCharacterOfPosition(current.getStart(source)).line + 1);
      ts.forEachChild(current, statements);
    };
    statements(node.body);
    results.push({language:'typescript', path:relative.replaceAll('\\','/'), qualified_name, kind, start_line:start, end_line:end, executable_lines:[...lines].sort((a,b)=>a-b), signature_hash:digest(signature), body_hash:digest(body), semantic_hash:digest(canonical)});
  };
  visit(source, []);
}
results.sort((a,b) => (a.path+':'+a.qualified_name).localeCompare(b.path+':'+b.qualified_name));
process.stdout.write(JSON.stringify(results));
