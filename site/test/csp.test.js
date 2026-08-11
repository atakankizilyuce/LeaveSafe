import { createHash } from 'node:crypto';

import { describe, expect, it } from 'vitest';

// The page as text, asked for the way vite hands over a file it should not try
// to interpret. Reading it off disk instead means resolving a path, and under
// jsdom a relative reference resolves against the document's base rather than
// this file — index.html becomes http://localhost:3000/index.html.
import html from '../index.html?raw';

/*
 * The policy in index.html makes two claims about the page, and both are the
 * kind that rot without anyone noticing.
 *
 * The first is a hash of the module that boots the page. Change that module by
 * a character and the browser refuses to run it: no demonstration, no intro, no
 * backdrop, and a page that still looks like a page until you wonder why
 * nothing moves. Nothing in a build step catches that, because there is no
 * build step.
 *
 * The second is that everything the page loads, it ships. That is what lets the
 * policy say 'self' and mean it, and it stops being true the first time someone
 * reaches for a font or a script from a CDN.
 */

const page = new DOMParser().parseFromString(html, 'text/html');
const policy = page.querySelector('meta[http-equiv="Content-Security-Policy"]')?.content ?? '';

describe('the content security policy', () => {
    it('is there at all', () => {
        expect(policy).toContain("default-src 'none'");
    });

    it('names the hash of the module the page boots from', () => {
        const inline = [...page.querySelectorAll('script:not([src])')];
        expect(inline).toHaveLength(1);

        // The parser has already normalised line endings, which is also what a
        // browser hashes — so this is the digest the browser will compute.
        const digest = createHash('sha256').update(inline[0].textContent, 'utf8').digest('base64');
        expect(policy).toContain(`'sha256-${digest}'`);
    });

    it('is told the truth by the page: nothing is fetched from off-site', () => {
        const fetched = [...page.querySelectorAll('script[src], link[href], img[src]')].map(
            (element) => element.getAttribute('src') ?? element.getAttribute('href'),
        );

        expect(fetched.length).toBeGreaterThan(0);
        for (const source of fetched) {
            expect(source).not.toMatch(/^(?:[a-z]+:)?\/\//i);
        }
    });
});
