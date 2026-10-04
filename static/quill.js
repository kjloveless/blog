const container = document.querySelector('[data-quill]');
const form = container.closest('form');
const savedContent = container.textContent;
container.textContent = '';

const quill = new Quill(container, {
    theme: 'snow',
    readOnly: !form,
    formats: ['header', 'bold', 'italic', 'underline', 'link', 'list', 'image'],
    modules: {
        toolbar: form ? [
            [{ header: [1, 2, 3, false] }],
            ['bold', 'italic', 'underline', 'link'],
            [{ list: 'ordered' }, { list: 'bullet' }],
            ['clean', 'image']
        ] : false
    }
});

loadContent(savedContent);

function loadContent(content) {
    try {
        const documentData = JSON.parse(content);
        if (Array.isArray(documentData?.ops)) {
            quill.setContents(documentData);
            return;
        }
    } catch {
        // Older articles contain plain text rather than Quill JSON.
    }
    quill.setText(content);
}

if (form) {
    const fields = form.querySelector('fieldset');
    const imageInput = form.querySelector('#image-input');
    const status = form.querySelector('#upload-status');
    const toolbar = quill.getModule('toolbar');
    let imageIndex = 0;

    toolbar.container.querySelector('.ql-image').setAttribute('aria-label', 'Add image');
    toolbar.addHandler('image', () => {
        imageIndex = quill.getSelection(true)?.index ?? quill.getLength() - 1;
        imageInput.value = '';
        imageInput.click();
    });

    imageInput.addEventListener('change', async () => {
        const file = imageInput.files[0];
        if (!file) return;
        if (file.size >= 10 * 1024 * 1024) {
            status.textContent = 'Please choose an image smaller than 10 MB.';
            return;
        }

        fields.disabled = true;
        quill.disable();
        status.textContent = 'Uploading image…';

        try {
            const body = new FormData();
            body.append('file', file);
            const response = await fetch('/upload', { method: 'POST', body });
            if (!response.ok) throw new Error(await response.text() || 'Image upload failed.');

            const { location } = await response.json();
            if (typeof location !== 'string' || !location.startsWith('/images/')) {
                throw new Error('The server did not return an image URL.');
            }
            // Programmatic updates work while typing is disabled.
            quill.insertEmbed(imageIndex, 'image', location);
            quill.setSelection(imageIndex + 1);
            status.textContent = '';
        } catch (err) {
            status.textContent = err.message || 'Image upload failed. Please try again.';
        } finally {
            fields.disabled = false;
            quill.enable();
        }
    });

    form.addEventListener('submit', (event) => {
        if (fields.disabled) {
            event.preventDefault();
            return;
        }
        form.elements.content.value = JSON.stringify(quill.getContents());
    });
}
