var config = new Object;
var arrayOfContent = [];
var lists = []
var savebutton = false;
var countTimeoutDefault = 300

const item_list_selector = 'item-list-selector'
const subject_list_selector = 'subject-list-selector'
const new_subject_list_selector = 'new-subject-list-selector'
var currentFilename = undefined
var previousFilename = undefined

const DEBUG = true

const skipsave = false
const restrictedsave = false
const showsavealert = false
//const BASE='lists/'

function resetCounter() {
    $("#staleCount").html(countTimeoutDefault)
}

function SaveList(content, filename, sb) {
    if (filename == undefined) {
        throw error
        //return
    }

    savebutton = (sb != undefined && sb)

    DEBUG && console.log("SaveList(...content...," + filename + ");");
    $('#saveButton').prop('disabled', true);

    if (restrictedsave) {
        console.log("-- RESTRICTED SAVE MODE, for safety! --")
        if (!filename.includes("test")) {
            $('#saveButton').prop('disabled', false);
            return
        }
    }
    console.log("roEnabled=" + $("#roEnable").is(":checked"))

    if ($("#roEnable").is(":checked")) {
        console.log("r/o mode!")
    } else {
        console.log("r/w mode!")
    }

    if (skipsave || $("#roEnable").is(":checked")) {
        console.log("-- SKIPPING SAVE, for safety! --")
        $('#saveButton').prop('disabled', false);
        return
    }

    dropVars(); //potential bug; this function works on arrayOfContent global rather than content local
    DEBUG && console.log(JSON.stringify(content, null, 3));

    DEBUG && console.log("OVERWRITING your data in " + filename);

    $.ajax({
        url: 'items/' + filename,  //relative target route
        type: 'post',  //method
        dataType: 'json',  //type to receive... json
        contentType: 'application/json', //contenttype to send
        success: function (data) {
            $('#saveButton').prop('disabled', false);
            console.log("success in saving content for filename: " + this.url)
            if (showsavealert || savebutton) {
                alert(data.msg)
                savebutton = false;
            }
        },
        data: JSON.stringify(content), // content to send; has to be stringified, even though it's application/json
        error: function (err) {   //something bad happened and ajax is unhappy
            console.log(JSON.stringify(err, null, 3));
            if (showsavealert) alert(err.responseJSON.error);
        }

    }).done(function (data) {
        console.log("done");
        //re-enable save button
        $('#saveButton').prop('disabled', false);

    });
}

async function loadList(fn) {
    console.log("loadList(" + fn + ")")
    var allContent = await ajaxGetJSON(fn)
    if (Array.isArray(allContent)) {
        console.log("loadList(" + fn + ") returning just array with length = " + allContent.length)
        return allContent;
    }
    if (allContent.title != undefined) {
        $("#titleDiv").show();
        $("#titleDiv").html(allContent.title)

        console.log("setting title to " + allContent.title)
    }
    //}
    console.log("loadList(" + fn + ") setting title and returning array with length = " + allContent.list.length)
    return allContent.list
}


function saveit(sb) {
    if ($("#titleDiv").text() == "") {
        if (currentFilename != undefined) SaveList(arrayOfContent, currentFilename, sb);
        return
    }

    var data = new Object()

    data.list = arrayOfContent
    data.title = $("#titleDiv").text();
    if (currentFilename != undefined) SaveList(data, currentFilename, sb);

}


async function SelectNewFile(nf) {
    if (nf == undefined) {
        throw error
    }

    console.log("====SelectNewFile(" + nf + ")")


    var p = currentFilename;
    console.log("\tcurrent file is " + currentFilename)
    console.log("\tselected file is " + lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()])


    var newsubject = nf.includes('/') && nf.split('/')[0] || lists[$('#' + subject_list_selector).val()].subject
    console.log("\twith new subject: " + newsubject)

    if (!nf.includes('/')) nf = newsubject + '/' + nf


    console.log("\tdesired file is " + nf)




    if (nf != undefined) {
        console.log("size of lists is " + lists.length)

        //if current subject and desired subject (within new filename nf, then select it)
        if (lists[$('#' + subject_list_selector).val()].subject != newsubject) {
            //console.log("need new subject: "+newsubject)

            for (var i = 0; i < lists.length; i++) {
                if (lists[i].subject == newsubject) {
                    $('#' + subject_list_selector).val(i)
                    //this.subjectListIndex=i
                    console.log("found and selected new subject (" + newsubject + ")")

                    rebuildListSelector(subject_list_selector, lists, newsubject)

                    continue;
                }
            }
        } else {

            console.log("sticking with same subject (" + newsubject + ")")

            // console.log("\t so " + nf + " was not in the list, let's append it")
            // lists[$('#' + subject_list_selector).val()].entries.push(nf);
            // console.log("size of lists is " + lists.length)
            // console.log("size of lists[" + $('#' + subject_list_selector).val() + "].entries is " + lists[$('#' + subject_list_selector).val()].entries.length)

        }


        //check to see if nf is in the list of entries
        var found = false
        for (var i = 0; i < lists[$('#' + subject_list_selector).val()].entries.length; i++) {
            if (lists[$('#' + subject_list_selector).val()].entries[i] == nf) {
                $('#' + item_list_selector).val(i)
                console.log("found and selected new item")
                found = true
                break;
            }
        }
        if ( !found ) {
            console.log("\t so " + nf + " was not in the list, let's append it")
            lists[$('#' + subject_list_selector).val()].entries.push(nf);
            console.log("size of lists is " + lists.length)
            console.log("size of lists[" + $('#' + subject_list_selector).val() + "].entries is " + lists[$('#' + subject_list_selector).val()].entries.length)
        }


    }

    //either the subject didn't change or it did; our lists should be correct now
    console.log("current subject index = " + $('#' + subject_list_selector).val());
    console.log("current list index = " + rebuildListSelector(item_list_selector, lists[$('#' + subject_list_selector).val()].entries, nf))

    if (currentFilename != undefined && config.autosave) SaveList(arrayOfContent, currentFilename);

    $("#titleDiv").text("")
    $("#titleDiv").hide()

    //arrayOfContent=await ajaxGetJSON('items/'+lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()]);
    arrayOfContent = await loadList('items/' + lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()]);
    currentFilename = lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()];
    render();

    $("#backBTN").prop("disabled", p == currentFilename);
    if (p == currentFilename) previousFilename = undefined;
    else previousFilename = p;
}

function SaveAndLoad(newfilename) {
    SelectNewFile(newfilename);
}

function changeItem() {
    SelectNewFile(lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()]);
}


function changeSubject() {
    SelectNewSubject(lists[$('#' + subject_list_selector).val()].subject, lists[$('#' + subject_list_selector).val()].subject + "/index." + config.ext)
}

function SelectNewSubject(newsubject, newfile) {

    if (newsubject == undefined && newfile == undefined) {
        console.log("neither subject nor file is defined; checking defaults")
        newfile = config.defaultItem || (config.defaultSubject + "/index." + config.ext);
        newsubject = tj.split('/')[0]
    }

    console.log("want to change subject to " + newsubject + " and load list " + newfile)
    // for(var i=0; i<lists.length; i++) {
    //     if(lists[i].subject==newsubject) {
    //         $('#'+subject_list_selector).val(i)
    //         console.log("found and selected new subject")
    //     }
    //     tj=lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()]
    // }

    //subjectListIndex=$('#'+subject_list_selector).val();

    // console.log("changeSubjectList(): fire rebuild list-selector")

    // var t=lists[$('#'+subject_list_selector).val()].subject

    // if(tj.startsWith(t)) {
    //     console.log("tj DOES start with "+t)
    // } else {
    //     console.log("tj does NOT start with "+t)

    //     tj=t+"/index."+config.ext
    // }
    // console.log("\tcalling rebuildListSelector with tj="+tj)

    rebuildListSelector(item_list_selector, lists[$('#' + subject_list_selector).val()].entries, newfile)


    //newFilename=lists[$('#subject-list-selector').val()].entries[$('#list-selector').val()]
    //changeItem();
    SelectNewFile(lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()]);

}

function revertList() {
    if (previousFilename != undefined) {
        console.log("calling SelectNewFile(" + previousFilename + ")")

        SelectNewFile(previousFilename);
    }
    $("#backBTN").prop("disabled", true);
}


async function startTodo(params) {
    console.log("enter start todo")
    resetCounter()
    console.log("reset counter")
    reduceCountDown()
    console.log("reduce countdown")

    console.log("load configuration file")

    //load /config into memory
    config = await ajaxGetJSON("config/");


    console.log("config looks like:")
    console.log(JSON.stringify(config, null, 3))


    console.log("about to call /items")
    //load items into memory
    lists = await ajaxGetJSON("items");

    console.log("lists looks like:")
    console.log(JSON.stringify(lists, null, 3))

    if (params != undefined) {
        if (params.subject != undefined) {
            config.defaultSubject = params.subject
        }
        if (params.item != undefined) {
            console.log("defaultItem=" + params.item)
            if (params.item.includes('/')) {
                console.log("NO need to include /")
                config.defaultItem = params.item
            } else {
                console.log("need to include /")
                config.defaultItem = config.defaultSubject + "/" + params.item
            }
        }
        if (params.readonly) {
            $("#roEnable").prop('checked', true)
            $("#saveButton").prop("disabled", true);
            $("#addButton").prop("disabled", true);
        } else {
            $("#roEnable").prop('checked', false)
            $("#saveButton").prop("disabled", false);
            $("#addButton").prop("disabled", false);
        }
    }
    //render selectors
    DEBUG && console.log("config.defaultSubject=" + config.defaultSubject)
    rebuildListSelector(subject_list_selector, lists, config.defaultSubject)

    rebuildListSelector(new_subject_list_selector, lists, config.defaultSubject)

    DEBUG && console.log("config.defaultItem=" + config.defaultItem)
    rebuildListSelector(item_list_selector, lists[$('#' + subject_list_selector).val()].entries, config.defaultItem)

    //load default topic and json
    currentFilename = lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()]
    previousFilename = undefined // on purpose, also disable back button
    $("#backBTN").prop("disabled", true);
    DEBUG && console.log("loading: " + currentFilename)
    //arrayOfContent=await ajaxGetJSON('items/'+currentFilename)

    arrayOfContent = await loadList('items/' + currentFilename)
    //render it
    render();
    $(".completeClass").hide()
}

async function reduceCountDown() {
    if ($("#cntEnable").is(':checked')) {
        if (isNaN($("#staleCount").html())) {
            $("#staleCount").html(countTimeoutDefault)
        }
        countTimeout = $("#staleCount").html() - 1
        $("#staleCount").html(countTimeout)
        if (countTimeout == 0) {
            arrayOfContent = await loadList('items/' + currentFilename)
            render()
            $(".completeClass").hide()
            alert("This instance has gone stale, reloading...")
            countTimeout = countTimeoutDefault
        }
        setTimeout(reduceCountDown, 1000)
    } else {
        $("#staleCount").html("n/a")
    }
}

function addIt() {
    console.log("add new item");
    var object = {};

    object.name = $("#itemName").val();
    object.votes = 0;
    object.skip = false;
    if ($("#itemJSON").val() != "") object.json = $("#itemJSON").val();

    if ($("#itemPeriod").val() != "") {
        object.periodic = true;
        object.period = Number($("#itemPeriod").val());
        var now = new Date();
        var dtom = DaysToMS(object.period)
        console.log("days=" + object.period);
        console.log("ms=" + dtom);
        console.log("ms(now)=" + now.getTime());
        var dueDate = new Date(now.getTime() + dtom);
        object.nextDue = dueDate.getTime();

        console.log("next due is " + object.nextDue);
        console.log("\tas date: " + EpocMStoISODate(object.nextDue));
    }
    arrayOfContent.push(object);

    $("#itemName").val("");
    $("#itemJSON").val("");
    $("#itemPeriod").val("");
    render();
    saveit();
    $("#addDiv").hide()
}

function showAdd() {
    $("#addDiv").toggle()
}

function calcNewHREF() {

    if ($("#roEnable").is(":checked")) extra = "&readonly=true";
    else extra = "";

    return window.location.origin + "/?subject=" + lists[$('#' + subject_list_selector).val()].subject + "&item=" + lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()] + extra;
}

function copyLink() {
    console.log(window.location)
    console.log("current item=" + lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()]);
    console.log("current subject=" + lists[$('#' + subject_list_selector).val()].subject);
    //copyToClipBoard("something?subject="+lists[$('#'+subject_list_selector).val()].subject+"&item="+lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()])
    console.log("new href=" + calcNewHREF())
    copyToClipBoard(calcNewHREF())
}

async function reloadUI(newsub, newfile) {
    //before we select a new subject and item
    lists = await ajaxGetJSON("items");
    rebuildListSelector(subject_list_selector, lists, newsub)
    rebuildListSelector(new_subject_list_selector, lists, newsub)
    rebuildListSelector(item_list_selector, lists[$('#' + subject_list_selector).val()].entries, newfile)
    SelectNewSubject(newsub, newfile)
}

function moveListToNewSubject() {
    var A = lists[$('#' + subject_list_selector).val()].entries[$('#' + item_list_selector).val()]
    var B = lists[$('#' + new_subject_list_selector).val()].subject;
    console.log("want to move " + A + " to subject " + B);

    //app.post('/items/:subject/:item/:newsubject',function(req,res) {
    console.log("POST /items/" + A + "/" + B);

    $.ajax({
        url: 'items/' + A + "/" + B,  //relative target route
        type: 'post',  //method
        dataType: 'json',  //type to receive... json
        contentType: 'application/json', //contenttype to send
        success: function (data) {
            $('#saveButton').prop('disabled', false);
            console.log("success in saving content for filename: " + this.url)
            if (showsavealert || savebutton) {
                alert(data.msg)
                savebutton = false;
            }

            //trigger subject change


            const listing = this.url.split('/')
            console.log("splitting url=" + this.url + " by /")
            console.log("length is " + listing.length)
            //function SelectNewSubject(newsubject,newfile) {
            console.log("SelectNewSubject(newsubject,newfile)")
            console.log("SelectNewSubject(" + listing[3] + "," + listing[3] + "/" + listing[2] + ")")

            reloadUI(listing[3], listing[3] + "/" + listing[2])


        },
        //data: JSON.stringify(content), // content to send; has to be stringified, even though it's application/json
        error: function (err) {   //something bad happened and ajax is unhappy
            console.log(JSON.stringify(err, null, 3));
            if (showsavealert) alert(err.responseJSON.error);
        }

    }).done(function (data) {
        console.log("done");
        //re-enable save button
        $('#saveButton').prop('disabled', false);

    });
}

function hideShowCompleted() {
    console.log("hide/show [completed]")

    $(".completeClass").toggle()
}
function hideShowBlocked() {
    console.log("hide/show [blocked]")

    $(".blockedClass").toggle()
}

// ── SSE: real-time sync ───────────────────────────────────────────────────────
var _sseSource = null;

function sseReload() {
    if (!currentFilename) { return; }
    loadList("items/" + currentFilename).then(function(content) {
        arrayOfContent = content;
        render();
    });
}

function connectSSE(subject) {
    if (_sseSource) { _sseSource.close(); }
    _sseSource = new EventSource("/api/events?subject=" + encodeURIComponent(subject));
    _sseSource.addEventListener("message", sseReload);
    _sseSource.onerror = function() {
        _sseSource.close();
        setTimeout(function() { connectSSE(subject); }, 3000);
    };
}