# Framework Detection — Build Commands & Output Directories

Use this table to determine the correct build command and output directory for Cloudflare Pages deployment.

| Build Tool | Build Command | Output Directory |
|-----------|---------------|-----------------|
| Vite | `vite build` | `dist/` |
| Next.js (static) | `next build && next export` | `out/` |
| Next.js (standalone) | `next build` | `.next/` (not suitable for Pages -- warn user) |
| Create React App | `react-scripts build` | `build/` |
| Remix | `remix build` | `build/client/` |
| Astro | `astro build` | `dist/` |
| SvelteKit | `vite build` | `build/` |
| Angular | `ng build` | `dist/<project>/` |
| Nuxt (static) | `nuxt generate` | `.output/public/` |
| Hugo | `hugo` | `public/` |
| Gatsby | `gatsby build` | `public/` |

## Detection Tips

- **Vite**: Look for `vite` in devDependencies and `vite.config.*` for custom `build.outDir`
- **Next.js**: Check `next.config.*` for `output: 'export'` (static) vs default (standalone)
- **Angular**: The output lands in `dist/<project-name>/` where project-name comes from `angular.json`
- **Nuxt**: Check `nuxt.config.*` for `ssr: false` or `target: 'static'`
- **Hugo/Gatsby**: These use `public/` by default but can be customized in their config files
