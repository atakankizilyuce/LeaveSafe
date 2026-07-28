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
        <div class="sheet-scrim" role="dialog" aria-modal="true" aria-label={label}>
            <button type="button" class="scrim-close" aria-label="Close" onClick={onClose} />
            {children}
        </div>
    );
}
