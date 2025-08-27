document.addEventListener('DOMContentLoaded', () => {
    function createUUID() {
        return ([1e7]+-1e3+-4e3+-8e3+-1e11).replace(/[018]/g, c =>
            (c ^ crypto.getRandomValues(new Uint8Array(1))[0] & 15 >> c / 4).toString(16)
        );
    }
    document.getElementById('create-doc').addEventListener('click', () => {
        window.location.href = `doc.html#${createUUID()}`;
    });
    document.getElementById('join-doc').addEventListener('click', () => {
        const docId = document.getElementById('join-id').value.trim();
        if (docId) { window.location.href = `doc.html#${docId}`; }
    });
});