import type { ComponentChildren } from 'preact';
import { useEffect } from 'preact/hooks';

interface Props {
    onClose(): void;
    label: string;
    children: ComponentChildren;
}

/**
 * The dimmed backdrop behind a sheet or dialog.
 *
 * Escape is the real way out and is bound here, so every overlay in the app
 * gets it for free. Clicking the backdrop is a pointer convenience on top of
 * that, not the only exit — which is what makes the click handler on a plain
 * element acceptable rather than a keyboard trap.
 *
 * It is a `<dialog>` held open by the attribute rather than one opened with
 * `showModal()`, and that is a deliberate refusal rather than a half-measure.
 * `showModal()` moves an element into the browser's top layer, which renders
 * above every z-index on the page — including the alert overlay at 60. The
 * alert and this sheet are independent siblings in the tree: nothing closes an
 * open settings sheet when an alert arrives. So a modal sheet would cover the
 * one screen this program exists to show, and it would do it only to the user
 * who happened to be in settings at the moment someone touched their laptop.
 * The element brings the semantics; the top layer is not worth that.
 */
export function Scrim({ onClose, label, children }: Props) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onClose();
        };
        document.addEventListener('keydown', onKey);

        // Stop the panel behind from scrolling under the sheet.
        const previous = document.body.style.overflow;
        document.body.style.overflow = 'hidden';

        return () => {
            document.removeEventListener('keydown', onKey);
            document.body.style.overflow = previous;
        };
    }, [onClose]);

    return (
        <dialog class="sheet-scrim" open aria-modal="true" aria-label={label}>
            <button type="button" class="scrim-close" aria-label="Close" onClick={onClose} />
            {children}
        </dialog>
    );
}
