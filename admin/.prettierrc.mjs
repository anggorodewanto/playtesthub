/** @type {import("prettier").Config} */
export default {
  printWidth: 140,
  semi: false,
  singleQuote: true,
  trailingComma: 'none',
  bracketSpacing: true,
  bracketSameLine: true,
  arrowParens: 'avoid',
  plugins: ['prettier-plugin-organize-imports']
};
