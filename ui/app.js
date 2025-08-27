window.addEventListener('DOMContentLoaded', () => {
    const editor = document.getElementById('editor');
    const statusEl = document.getElementById('status');
    const userListEl = document.getElementById('user-list');
    const usernameInput = document.getElementById('username');
    const docTitleEl = document.getElementById('doc-title');

    let localDoc = [];
    const clientID = 'client-' + Math.random().toString(36).substr(2, 9);
    let username = 'User-' + clientID.substr(0, 4);
    usernameInput.value = username;

    const defaultDocID = "123e4567-e89b-12d3-a456-426614174000";
    let docID = window.location.hash.substring(1);
    function isValidUUID(uuid) { return /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(uuid); }
    if (!isValidUUID(docID)) { docID = defaultDocID; window.location.hash = docID; }
    docTitleEl.textContent = `Document: ${docID}`;

    const socket = new WebSocket('ws://' + window.location.host.split(':')[0] + ':8080/ws');
    socket.onopen = () => {
        statusEl.textContent = 'Connected to Agent'; statusEl.className = 'font-mono text-green-400';
        socket.send(JSON.stringify({ action: 'join', docID: docID }));
        setInterval(sendPresence, 5000); sendPresence();
    };

    socket.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (data.action === 'load') {
            localDoc = data.doc;
            render(editor.selectionStart);
            socket.send(JSON.stringify({action: "save_snapshot", doc: localDoc}));
        } else if (data.action === 'presence_update') {
            updateUserList(data.users);
        } else if (data.action === 'crdt_insert' || data.action === 'crdt_delete') {
            if (data.clientID === clientID) return;
            applyOperation(data);
        }
    };

    usernameInput.addEventListener('change', () => { username = usernameInput.value.trim() || 'User-' + clientID.substr(0, 4); sendPresence(); });
    function sendPresence() { if (socket.readyState === WebSocket.OPEN) { socket.send(JSON.stringify({ action: 'presence', clientID, username })); } }
    function updateUserList(users) { userListEl.innerHTML = ''; if(!users) return; for (const [id, name] of Object.entries(users)) { const li = document.createElement('li'); li.textContent = name; li.className = `text-slate-400 ${id === clientID ? 'font-bold text-cyan-300' : ''}`; userListEl.appendChild(li); } }

    editor.addEventListener('input', (e) => {
        const editorText = editor.value;
        const modelText = localDoc.map(c => c.value).join('');
        let i = 0;
        while (i < editorText.length && i < modelText.length && editorText[i] === modelText[i]) { i++; }
        if (modelText.length > editorText.length) {
            for (let j = 0; j < modelText.length - editorText.length; j++) {
                socket.send(JSON.stringify({ action: 'raw_delete', clientID, index: i }));
            }
        } else if (editorText.length > modelText.length) {
            const inserted = editorText.slice(i, i + editorText.length - modelText.length);
            inserted.split('').forEach((char, offset) => {
                socket.send(JSON.stringify({ action: 'raw_insert', clientID, char: { value: char }, index: i + offset }));
            });
        }
        localDoc = editorText.split('').map(c => ({value: c})); // Simple model update
    });

    function applyOperation(op) {
        const alreadyExists = op.action === 'crdt_insert' && localDoc.some(c => c.id && c.id.peerID === op.char.id.peerID && c.id.clock === op.char.id.clock);
        if (alreadyExists) return;
        const cursorPos = editor.selectionStart;
        if (op.action === 'crdt_insert') {
            const index = findInsertIndex(op.char);
            localDoc.splice(index, 0, op.char);
        } else if (op.action === 'crdt_delete') {
            localDoc = localDoc.filter(c => !(c.id.peerID === op.char.id.peerID && c.id.clock === op.char.id.clock));
        }
        render(cursorPos);
    }
    
    function render(cursorPos) {
        const text = localDoc.map(c => c.value).join('');
        if (document.activeElement === editor) {
            editor.value = text;
            editor.setSelectionRange(cursorPos, cursorPos);
        } else {
            editor.value = text;
        }
    }

    function findInsertIndex(char) { let low = 0, high = localDoc.length; while (low < high) { const mid = Math.floor((low + high) / 2); if (!localDoc[mid].position || comparePositions(localDoc[mid].position, char.position) > 0) { high = mid; } else { low = mid + 1; } } return low; }
    function comparePositions(p1, p2) { for (let i = 0; i < p1.length && i < p2.length; i++) { if (p1[i] < p2[i]) return -1; if (p1[i] > p2[i]) return 1; } if (p1.length < p2.length) return -1; if (p1.length > p2.length) return 1; return 0; }
});