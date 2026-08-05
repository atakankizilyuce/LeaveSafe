import { defineConfig } from 'vitest/config';

// Deliberately separate from vite.config.ts. That file produces the committed
// web/dist, and CI fails if the committed output drifts from the source — so it
// stays free of anything only the tests need.
export default defineConfig({
    test: {
        // The service worker is driven against a stand-in for the worker global
        // scope rather than a browser, so no DOM is needed.
        environment: 'node',
        include: ['test/**/*.test.js'],
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
