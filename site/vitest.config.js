import { defineConfig } from 'vitest/config';

/*
 * The page itself is still three files with no build step — this is only how
 * the demonstration is tested.
 *
 * Running it with the vitest already installed in web/ was tried first and does
 * not work: vitest resolves its own coverage provider from the test root, not
 * from the working directory, so `--root site` looks for @vitest/coverage-v8
 * beside the page and finds nothing. An alias pointing into web/node_modules
 * would have papered over that, at the cost of a config that breaks whenever
 * either directory moves. A package.json is the boring answer.
 */
export default defineConfig({
    test: {
        // demo.js speaks to nothing but the DOM, so there is a DOM.
        environment: 'jsdom',
        include: ['test/**/*.test.js'],
        coverage: {
            provider: 'v8',
            reporter: ['text', 'lcov'],
            reportsDirectory: 'coverage',
            // index.html and style.css have no statements to cover, and asking
            // for a number on them would only produce one that means nothing.
            //
            // intro.js is left out for a different reason: it is a canvas and a
            // scroll listener, and jsdom has neither a 2d context nor a
            // viewport that scrolls. A test of it could only assert that
            // nothing threw, and counting that as coverage would make the
            // number worse than no number. Its arithmetic lives in
            // ascii-mark.js, which is here and is tested.
            include: ['demo.js', 'ascii-mark.js'],
        },
    },
});
