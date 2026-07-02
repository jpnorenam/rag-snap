/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static SPA export: `next build` emits a directory of static files (ui/out)
  // with no Node.js runtime, so the assets can be embedded into ragd via
  // go:embed and served as plain files.
  output: 'export',
  // The daemon serves the UI under /ui/ same-origin with the API, so every
  // asset and route must be prefixed accordingly (LXD's /ui/ model).
  basePath: '/ui',
  // Emit directory-style routes (foo/index.html) so the SPA fallback can map a
  // deep link to a real file when one exists.
  trailingSlash: true,
};

module.exports = nextConfig;
