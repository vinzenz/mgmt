/* NOTES:
 *
 * https://copr.fedorainfracloud.org/coprs/g/cockpit/cockpit-preview/
 * npm install <whatever>
 *
 * cockpit-bridge --packages # list what's there
 * ~/.local/share/cockpit/mgmt/ # contains manifest.json mgmt.html
 *
 * ./node_modules/.bin/webpack
 * ./node_modules/.bin/webpack mgmt.js mgmtbundle.js // make it
 * ./node_modules/.bin/webpack mgmt.js mgmtbundle.js --watch  // make it live
 *
 * http://localhost:9090/mgmt/mgmt
 *
 */

console.log("mgmt + COCKPIT");

require("./patterns.js"); // additional cockpit provided widgets

var jsyaml = require('js-yaml'); // r.js is needed to require

var YAML = { stringify: jsyaml.safeDump, parse: jsyaml.safeLoad }; // thanks mvollmer

var basepath = "/home/james/.local/share/cockpit/mgmt/"; // XXX: is there an API to get this path?

$(document).ready(main); // required!

function main() {

	$("#save").on('click', write);

	file = cockpit.file(basepath + "mgmt.yaml", { syntax: YAML });

	file.watch(watch); // XXX: we seem to get one event on load... is this guaranteed?

	promise = file.read(); // XXX: read once anyways...
	promise
	.done(write)
	.fail(error);

	$("#myslider").on('change', updateslider);
	$("#myslidertext").on('change', updateslidertext );

}

function updateslider(event) {
	$("#myslidertext").val( event.currentTarget.value );
}

function updateslidertext(event) {
	console.log("XXX: ", parseFloat(event.currentTarget.value));
	$("#myslider").prop("value", parseFloat(event.currentTarget.value)); // XXX: doesn't work
}

function watch(content, tag, error) {
	// XXX: if error ...
	console.log("FILE CHANGED", content, tag);
	read(content, tag)
}

function read(content, tag) {
	console.log("Data:", content.hello);
	$("#comment").val( content.comment );
	$("#myslidertext").val( parseFloat(content.ratio) );
}

function write() {
	console.log("Writing...");

	newcontent = {
		"hello": "world",
		"ratio": parseFloat($("#myslidertext").val()),
		"comment": $("#comment").val(),
	};

	promise = file.replace(newcontent); // [ expected_tag ])
	promise
	.done(function (new_tag) {
		$("#info").val( "file updated successfully!" );
	})
	.fail(error);
}

function error(err) {
	$("#info").val( "file error: " + err );
}

//promise = file.replace(content, [ expected_tag ])
//promise
//    .done(function (new_tag) { ... })
//    .fail(function (error) { ... })

//promise = file.modify(callback, [ initial_content, initial_tag ]
//promise
//    .done(function (new_content, new_tag) { ... })
//    .fail(function (error) { ... })

//file.close()
