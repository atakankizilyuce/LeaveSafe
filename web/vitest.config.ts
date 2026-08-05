import { defineConfig } from 'vitest/config';

// Deliberately separate from vite.config.ts. That file produces the committed
// web/dist, and CI fails if the committed output drifts from the source — so it
// stays free of anything only the tests need.
export default defineConfig({
    // The components are written in JSX and compiled against preact, the same
    // way tsconfig.json describes them. Without this the tests that render one
    // would be handed React's factory, which is not what preact ships.
    esbuild: {
        jsx: 'automatic',
        jsxImportSource: 'preact',
    },
    test: {
        // The service worker and the libraries are driven against stand-ins for
        // the globals they reach for rather than a browser, so no DOM is needed
        // by default. The few tests that render a component ask for jsdom with
        // an `@vitest-environment` docblock of their own, which keeps the cost
        // of a DOM on the files that actually need one.
        environment: 'node',
        include: ['test/**/*.test.{js,ts,tsx}'],
        coverage: {
            provider: 'v8',
            reporter: ['text', 'lcov'],
            reportsDirectory: 'coverage',
            // Every source file, not only the tested ones. A file with no test
            // should read as nought per cent rather than be left out of the
            // number entirely.
            include: ['public/**/*.js', 'src/**/*.ts', 'src/**/*.tsx'],
        },
    },
});
