function post(url, data) {
    return new Promise((resolve, reject) => {
        sendAPI(url, data, 'POST')
        .then(res => resolve(res))
        .catch(err => reject(err));
    });
}

function get(url, object) {
    return new Promise((resolve, reject) => {
        let query = new URLSearchParams(object).toString();
        sendAPI(url + '?' + query, null, 'GET')
        .then(res => resolve(res))
        .catch(err => reject(err));
    });
}

function put(url, data) {
    return new Promise((resolve, reject) => {
        sendAPI(url, data, 'PUT')
        .then(res => resolve(res))
        .catch(err => reject(err));
    });
}

function del(url, data) {
    return new Promise((resolve, reject) => {
        sendAPI(url, data, 'DELETE')
        .then(res => resolve(res))
        .catch(err => reject(err));
    });
}

function sendAPI(url, data, method) {
    return new Promise((resolve, reject) => {
        let d = data;
        if (d == null && method != 'GET') d = new FormData();
        fetch(url, {
            method: method,
            body: d,
            credentials: 'include'
        }).then(res => {
            if (res.status == 200) return res.text();
            else reject(res);
        }).then(txt => {
            try {
                resolve(JSON.parse(txt));
            } catch(err) {
                console.error(err);
                reject(err);
            }
        }).catch(err => {
            console.error(err);
            reject(err);
        });
    });
}

function formDisabled(form, dis) {
	if (dis) {
		Array.from(form.getElementsByTagName('input')).forEach(elm => elm.setAttribute('disabled', ''));
		Array.from(form.getElementsByTagName('textarea')).forEach(elm => elm.setAttribute('disabled', ''));
		Array.from(form.getElementsByTagName('button')).forEach(elm => elm.setAttribute('disabled', ''));
		Array.from(form.getElementsByTagName('select')).forEach(elm => elm.setAttribute('disabled', ''));
        Array.from(form.querySelectorAll('input[type="checkbox"]')).forEach(elm => elm.setAttribute('onclick', 'return false;'));
        Array.from(form.querySelectorAll('input[type="radiobutton"]')).forEach(elm => elm.setAttribute('onclick', 'return false;'));
	} else {
		Array.from(form.getElementsByTagName('input')).forEach(elm => elm.removeAttribute('disabled'));
		Array.from(form.getElementsByTagName('textarea')).forEach(elm => elm.removeAttribute('disabled'));
		Array.from(form.getElementsByTagName('button')).forEach(elm => elm.removeAttribute('disabled'));
		Array.from(form.getElementsByTagName('select')).forEach(elm => elm.removeAttribute('disabled'));
        Array.from(form.querySelectorAll('input[type="checkbox"]')).forEach(elm => elm.removeAttribute('onclick'));
        Array.from(form.querySelectorAll('input[type="radiobutton"]')).forEach(elm => elm.removeAttribute('onclick'));
	}
}

function get2form(form) {
    let inputs = [];
    for (let i = 0; i < (inputs = form.getElementsByTagName('input')).length; i++) {
        if (inputs[i].getAttribute('type') == 'checkbox' || inputs[i].getAttribute('type') == 'radiobutton') {
            if (inputs[i].checked) inputs[i].click();
        }
    }
    new URL(location).searchParams.forEach((v, k) => {
        Array.from(document.getElementsByName(k)).forEach(elm => {
            if (elm.getAttribute('type') == 'checkbox' || elm.getAttribute('type') == 'radio') {
                if (elm.value == v) (!elm.checked ? elm.click() : 0);
            } else {
                elm.value = v;
            }
        });
    });
}