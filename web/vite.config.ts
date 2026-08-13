import preact from '@preact/preset-vite';
import { defineConfig } from 'vite';

// The build output is committed to web/dist and embedded in the Go binary, so a
// fresh clone builds a working leavesafe with no Node installed. CI rebuilds
// from source and fails if the committed output has drifted.
export default defineConfig({
    plugins: [preact()],
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        // Stable filenames. A hashed name would churn the committed dist on
        // every build and make the drift check unreadable in review.
        rollupOptions: {
            output: {
                entryFileNames: 'app.js',
                chunkFileNames: 'app-[name].js',
                assetFileNames: 'app.[ext]',
            },
        },
    },
    server: {
        // `npm run dev` serves the UI with hot reload and forwards the API to a
        // leavesafe running on 9443. Start it with PORT=9443, then run this —
        // the listener picks a free port otherwise and this would aim at
        // nothing. See docs/development.md.
        proxy: {
            '/ws': {
                target: 'ws://127.0.0.1:9443',
                ws: true,
                changeOrigin: true,
            },
        },
    },
});
