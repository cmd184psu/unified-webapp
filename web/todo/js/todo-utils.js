

var COOLDOWN_TIME=600000
const AUTO_SAVE_ENABLED=false
const AUTO_SAVE_PERIOD=60000
const VOTES_TO_CAST=100


var currentFilename="";


var priorFilename="";
var maxVotes=0;


//var globalData=[]  -- replace with arrayOfContent
var globalEL="thetable"
var draggedOver=-1
var dragging=-1

function reIndex() {
  for(var i=0; i<arrayOfContent.length; i++) {
      console.log("trying to set idx of i="+i+" out of "+arrayOfContent.length)
    arrayOfContent[i].idx=i;
  }
}



// function LoadFile(filename) {
// 	return new Promise((resolve, reject) => {

//         //$("#loaded").html(filename);
//         console.log("attempting to load "+$('lists-selelctor'))
//         $.get(BASE+filename,"", function(result) { resolve(result); });
//     });
// }

//moved to utils.js
// function ajaxGet(uri) {
// 	return new Promise((resolve, reject) => {
//         $.get(uri,"", function(result) { resolve(result); });
//     });
// }

// function ajaxGetJSON(uri) {
//     return new Promise((resolve, reject) => {
//         fetch(uri).then(async (response)=> {
//             resolve(await response.json())
//         })
//     })
// }    

function dropVars() {
    for(var i=0; i<arrayOfContent.length; i++) {
        arrayOfContent[i].votes=undefined;
        arrayOfContent[i].winner=undefined;
        arrayOfContent[i].resetWeight=undefined;
        arrayOfContent[i].currentWeight=undefined;
        arrayOfContent[i].value=undefined;
        arrayOfContent[i].winner=undefined;
        arrayOfContent[i].vote=undefined;
        arrayOfContent[i].expires=undefined;
        arrayOfContent[i].tempSkip=undefined;
        arrayOfContent[i].idx=undefined;
    }
}
function clearWinner() {
    for(var i=0; i<arrayOfContent.length; i++) {
        if(arrayOfContent[i].winner!=undefined) arrayOfContent[i].winner=undefined;
    }
}

//moved to utils.js
// function copyToClipBoard(text){
//     var c=document.getElementById('copytext');
//     c.value=text;
    
//     var x=document.getElementById('hiddentext');
//     x.style.display="block";
    
//         c.select();
//         try {
//       var successful = document.execCommand('copy')
//       var msg = successful ? 'successfully' : 'unsuccessfully'
//       alert('Copied!');
//         }catch(err) {
//       alert('Falied to copy.');
//         }
//         x.style.display="none";
//   }
  


function calcValue(v,tv) {
    return v/tv;
}

function TotalVotes(list) {
    var totalVotes=0;
    for(var i=0; i<list.length; i++) {
	    totalVotes+=list[i].votes;
    }
    return totalVotes;
}

function skipit(i) {
    arrayOfContent[i].skip=true;
    arrayOfContent[i].onHold=false;
    arrayOfContent[i].inProgress=false;
    arrayOfContent[i].completedOn=Math.round(Date.now() / 1000);
    render();
    saveit();
}

function dontskipit(i) {
    arrayOfContent[i].skip=false;
    delete arrayOfContent[i].completedOn;
    render();
    saveit();
}

function deleteit(i) {
    if($("#roEnable").is(":checked")) return;
    arrayOfContent.splice(i,1);  
    render();
    saveit();
}

function onHoldFlip(i) {
    if($("#roEnable").is(":checked")) return;
    arrayOfContent[i].onHold=!arrayOfContent[i].onHold;
    render();
    saveit();
}
function inProgressFlip(i) {
    if($("#roEnable").is(":checked")) return;
    arrayOfContent[i].inProgress=!arrayOfContent[i].inProgress;
    render();
    saveit();
}

//moved to utils
// function DaysToMS(days) {
// 	//return days*24*60*60*1000;
// 	return days*86400000;
// }

// function EpocMStoISODate(ms) {
// 	var d=new Date(ms);
// 	return formatedDate(d);
// }

// function isDueNow(ms) {
//     var now=new Date();
//     return ms<now.getTime();
// }

// function formatedDate(d) {
//     return (d.getMonth()+1)+"/"+d.getDate()+"/"+d.getFullYear();
// }

function resetDueDate(i) {
    var now=new Date();
	var dtom=DaysToMS(arrayOfContent[i].period)
	console.log("days="+arrayOfContent[i].period);
	console.log("ms="+dtom);
    console.log("ms(now)="+now.getTime());
    console.log("formated date(now)="+formatedDate(now))
	var dueDate=new Date(now.getTime()+dtom);
	arrayOfContent[i].nextDue=dueDate.getTime();
	console.log("next due is "+arrayOfContent[i].nextDue+" "+EpocMStoISODate(arrayOfContent[i].nextDue));
    render();
}



// function moveUp(i) {
//     if(i==0) {
//         console.log("already at end of list");
//     } else {
//         console.log("move i="+i+" to "+(i-1));

//         var temp=arrayOfContent[i];
//         arrayOfContent[i]=arrayOfContent[i-1];
//         arrayOfContent[i-1]=temp;
//         render();
//     }
// }
// function moveDown(i) {
//     if(i==arrayOfContent.length-1) {
//         console.log("already at end of list");
//     } else {
//         console.log("move i="+i+" to "+(i+1));
//         var temp=arrayOfContent[i];
//         arrayOfContent[i]=arrayOfContent[i+1];
//         arrayOfContent[i+1]=temp;
//         render();
//     }
// }

//Move to todo.js
function resetCoolDown(i) {
    arrayOfContent[i].expires=0;
    render();
}

//Move to todo.js
function isInCoolDown(item) {
    var d=new Date();
    return (item.expires!=undefined && item.expires>d.getTime());
}

//Move to utils.js
// function embedURL(str) {
//     const myArr = str.split(" ");
//     var newArray = [];
//     for(var i=0; i<myArr.length; i++) {
//         if(myArr[i].includes(";http")) {
//             newArray.push("<a href=\""+myArr[i].split(';')[1]+"\" target=_blank>"+myArr[i].split(';')[0]+"</a>")
//         } else {
//             newArray.push(myArr[i])
//         }
//     }
//     return newArray.join(" ")
// }

//not used
// function gripIt(i) {
//     console.log("got a grip on it for i="+i)
// }

//Move to todo.js
function renderRow(i) {
    trbit = '';
    var d = new Date();

    // Drag grip — stored separately so it is never overwritten below
    var grip = '<td class="drag-grip" title="Drag to reorder">' +
        '<svg viewBox="0 0 14 14" fill="currentColor" width="14" height="14" style="pointer-events:none">' +
        '<circle cx="4" cy="3" r="1.2"/><circle cx="10" cy="3" r="1.2"/>' +
        '<circle cx="4" cy="7" r="1.2"/><circle cx="10" cy="7" r="1.2"/>' +
        '<circle cx="4" cy="11" r="1.2"/><circle cx="10" cy="11" r="1.2"/>' +
        '</svg></td>';

    var trophy = '';
    if (arrayOfContent[i].winner) {
        trophy = '<span class="ctrl-icon"><i class="fas fa-trophy"></i></span>';
        arrayOfContent[i].expires = d.getTime() + COOLDOWN_TIME;
    }

    // Control panel — flex div, no nested table
    var ctrl = '<div class="ctrl-panel">';
    if (!$("#roEnable").is(":checked")) {
        ctrl += '<span class="ctrl-icon ctrl-delete" onclick="deleteit('+i+')"><i class="fa fa-trash"></i></span>';
    }
    if (arrayOfContent[i].skip || $("#roEnable").is(":checked")) {
        ctrl += trophy;
    } else {
        ctrl += '<span class="ctrl-icon" onclick="onHoldFlip('+i+')"><i class="fas fa-hand-paper"></i></span>';
        ctrl += '<span class="ctrl-icon" onclick="inProgressFlip('+i+')"><i class="fas fa-play"></i></span>';
        ctrl += '<span class="ctrl-icon" onclick="editFlip('+i+')"><i class="fas fa-edit"></i></span>';
        ctrl += trophy;
    }
    ctrl += '</div>';

    var row;
    if (arrayOfContent[i].skip || (!arrayOfContent[i].winner && isInCoolDown(arrayOfContent[i]))) {
        row  = grip;
        row += '<td><input type="checkbox" checked onclick="dontskipit('+i+')"/></td>';
        row += '<td>'+ctrl+'</td>';
        row += '<td>'+(i+1)+'</td>';
        row += '<td><s>'+arrayOfContent[i].name+'</s></td>';
    } else {
        row  = grip;
        row += '<td><input type="checkbox" onclick="skipit('+i+')"/></td>';
        row += '<td>'+ctrl+'</td>';
        row += '<td>'+(i+1)+'</td>';
        var n = arrayOfContent[i].name;
        var j = (arrayOfContent[i].json == undefined || arrayOfContent[i].json == "undefined") ? "" : arrayOfContent[i].json;
        row += '<td>';
        row += '<div id="nonediting'+i+'">'+renderItem(i)+'</div>';
        row += '<div id="editing'+i+'" style="display:none">';
        row += '<input size="50" type="text" value="'+n+'" onkeydown="saveeditName(this,'+i+')" />';
        row += '<input size="50" type="text" value="'+j+'" onkeydown="saveeditJSON(this,'+i+')" />';
        row += '</div></td>';
    }

    // Vote count
    row += '<td>' + (arrayOfContent[i].votes !== undefined ? arrayOfContent[i].votes : '') + '</td>';

    // Period / next due date
    if (arrayOfContent[i].skip) {
        row += '<td></td><td>'+EpocMStoISODate(arrayOfContent[i].completedOn*1000)+'</td>';
    } else if (arrayOfContent[i].periodic == undefined || !arrayOfContent[i].periodic) {
        row += '<td></td><td></td>';
    } else {
        var dueDate = new Date(arrayOfContent[i].nextDue);
        row += '<td>'+arrayOfContent[i].period+'</td>';
        row += '<td><div class="date-cell">'+formatedDate(dueDate)+
               '<span class="ctrl-icon" onclick="resetDueDate('+i+')"><i class="fas fa-sync-alt"></i></span></div></td>';
    }

    // Cooldown
    var coolDown = (arrayOfContent[i].expires == undefined || (arrayOfContent[i].expires - d.getTime() <= 0)) ? 'Ready' : 'Cool down';
    row += '<td><div class="date-cell">'+coolDown+
           '<span class="ctrl-icon" onclick="resetCoolDown('+i+')"><i class="fas fa-sync-alt"></i></span></div></td>';

    return trbit + row;
}


// ── Pointer-based drag-and-drop (grocery-list style) ─────────────────────────
// Rows stay locked in place; a line indicator shows the drop position.
// Only the grip handle initiates a drag.

var ntDrag = { active: false, srcIdx: null, srcRow: null };

function _ntClearIndicators() {
    document.querySelectorAll('#thetable tr.nt-drag-above, #thetable tr.nt-drag-below')
        .forEach(function(el) {
            el.classList.remove('nt-drag-above', 'nt-drag-below');
        });
}

function attachRowDrag(row, handle, idx) {
    function pointerStart() {
        if ($("#roEnable").is(':checked') || indexMode) return false;
        ntDrag.active = true;
        ntDrag.srcIdx = idx;
        ntDrag.srcRow = row;
        row.classList.add('nt-dragging');
        return true;
    }

    function pointerMove(clientX, clientY) {
        if (!ntDrag.active) return;
        _ntClearIndicators();
        var el = document.elementFromPoint(clientX, clientY);
        if (!el) return;
        var targetRow = el.closest('#thetable tr');
        if (targetRow && targetRow !== ntDrag.srcRow) {
            var rect = targetRow.getBoundingClientRect();
            targetRow.classList.add(
                clientY < rect.top + rect.height / 2 ? 'nt-drag-above' : 'nt-drag-below'
            );
        }
    }

    function pointerEnd(clientX, clientY) {
        if (!ntDrag.active) return;
        ntDrag.active = false;
        _ntClearIndicators();
        if (ntDrag.srcRow) ntDrag.srcRow.classList.remove('nt-dragging');

        var el = document.elementFromPoint(clientX, clientY);
        var targetRow = el && el.closest('#thetable tr');
        if (targetRow && targetRow !== ntDrag.srcRow) {
            var rect   = targetRow.getBoundingClientRect();
            var toIdx  = parseInt(targetRow.id.substr(4));
            if (clientY >= rect.top + rect.height / 2) toIdx++;
            var fromIdx = ntDrag.srcIdx;
            var p = arrayOfContent[fromIdx];
            arrayOfContent.splice(fromIdx, 1);
            arrayOfContent.splice(Math.max(0, fromIdx < toIdx ? toIdx - 1 : toIdx), 0, p);
            saveit();
            render();
        }
        ntDrag.srcIdx = null;
        ntDrag.srcRow = null;
    }

    // Mouse
    handle.addEventListener('mousedown', function(e) {
        if (!pointerStart()) return;
        e.preventDefault();
        var onMove = function(e) { pointerMove(e.clientX, e.clientY); };
        var onUp   = function(e) {
            pointerEnd(e.clientX, e.clientY);
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup',   onUp);
        };
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup',   onUp);
    });

    // Touch
    handle.addEventListener('touchstart', function(e) {
        if (!pointerStart()) return;
        var onMove = function(e) {
            var t = e.touches[0];
            pointerMove(t.clientX, t.clientY);
            e.preventDefault();
        };
        var onEnd = function(e) {
            var t = e.changedTouches[0];
            pointerEnd(t.clientX, t.clientY);
            handle.removeEventListener('touchmove', onMove);
            handle.removeEventListener('touchend',  onEnd);
        };
        handle.addEventListener('touchmove', onMove, { passive: false });
        handle.addEventListener('touchend',  onEnd);
    }, { passive: true });
}





function genRow(i) {
    var node = document.createElement("tr");
    var item = arrayOfContent[i];

    if (arrayOfContent[i].skip) {
        node.className = "draggableRow completeClass";
    } else if (arrayOfContent[i].onHold) {
        node.className = "draggableRow blockedClass";
    } else if (arrayOfContent[i].inProgress) {
        node.className = "draggableRow inProgressClass";
    } else {
        node.className = "draggableRow";
    }
    if (arrayOfContent[i].winner) node.classList.add('winnerClass');

    node.id = "list" + item.idx;
    node.innerHTML = renderRow(i);

    // Attach pointer-based drag to the grip handle only
    var grip = node.querySelector('.drag-grip');
    if (grip) attachRowDrag(node, grip, i);

    return node;
}
function eligibleToVote(item) {
    var d=new Date();
    if(item.skip || item.onHold) return false;
    if(item.expires!=undefined && item.expires-d.getTime()>0) return false;
    return true;
}

var _voteEnableTimer = null;

function updateVoteButton() {
    var hasEligible = arrayOfContent.some(function(item) { return eligibleToVote(item); });
    $('#voteBtn').prop('disabled', !hasEligible);

    // Clear any pending timer — we'll reschedule if still needed.
    if (_voteEnableTimer !== null) { clearTimeout(_voteEnableTimer); _voteEnableTimer = null; }

    if (!hasEligible) {
        // Find the earliest cooldown expiry among non-permanently-blocked items.
        var now = Date.now();
        var nextExpiry = Infinity;
        arrayOfContent.forEach(function(item) {
            if (!item.skip && !item.onHold && item.expires && item.expires > now) {
                nextExpiry = Math.min(nextExpiry, item.expires);
            }
        });
        if (nextExpiry !== Infinity) {
            _voteEnableTimer = setTimeout(function() {
                _voteEnableTimer = null;
                updateVoteButton();
            }, nextExpiry - now + 50);
        }
    }
}

function vote() {
    //clear the vote
    for (var i = 0; i < arrayOfContent.length; i++) {
        arrayOfContent[i].votes=0;
    }

    var rand = function(min, max) {
        return Math.floor(Math.random() * (max - min + 1)) + min;
    };
 
    var generateVoteList = function(list) {
        var vote_list = [];
        console.log("list length="+list.length);
        
        for (var i = 0; i < list.length; i++) {
            if(eligibleToVote(list[i]) ) {
                console.log("list["+i+"].name="+list[i].name);
                // Loop over the list of items
                vote_list.push(list[i].name);
                
            }
        }
        return vote_list;
    };

    //var list = ['javascript', 'php', 'ruby', 'python'];
    
    var vote_list = generateVoteList(arrayOfContent);

    console.log(vote_list.length);
    var random_num = 0;
    var extra=0;
    var t=VOTES_TO_CAST;
    var old_random_num=-1;
    var random_num=-1;


    clearWinner();
    maxVote_j=-1;


    switch(vote_list.length) {
        case 0:
            console.log("no votes were cast");
        break;
        case 1:
            for(var j=0; j<arrayOfContent.length; j++) {
                if(arrayOfContent[j].name==vote_list[0]) {
                    arrayOfContent[j].votes=VOTES_TO_CAST;
                    arrayOfContent[j].winner=true;
                }
            }
        break;
        default: // size 2 or bigger
            for(var i=0; i<VOTES_TO_CAST; i++) {
                while(old_random_num==random_num) {
                    random_num=rand(0, vote_list.length-1);
                }
                old_random_num=random_num;
                for(var j=0; j<arrayOfContent.length; j++) {
                    if(arrayOfContent[j].name==vote_list[random_num]) {
                        arrayOfContent[j].votes++;
                    } else {
                        console.log("skipped: "+arrayOfContent[j].name)
                    }
                }
            }
        break;

    }
    total_vote_count=t;
    //console.log("extra votes: "+extra);
    
    maxVotes=0;
    maxVote_j=-1;
    for(var j=0; j<arrayOfContent.length; j++) {
        if(arrayOfContent[j].votes>maxVotes) {
            maxVotes=arrayOfContent[j].votes
            maxVote_j=j;
        }
    }

    if(maxVote_j>-1) {
        console.log("the winner is: "+arrayOfContent[maxVote_j].name+" with "+arrayOfContent[maxVote_j].votes)
        arrayOfContent[maxVote_j].winner=true;
    } else {
        console.log("no winner declared")
    }
   
    maxVotes=0;
    setTimeout(render, 0);
}

//moved to utils.js
// function genTableHeader(arr) {
//     //console.log("get here")
//     //row to return
//     var ret=document.createElement("tr");  

    
//     for(var i=0; i<arr.length; i++) {
//         console.log("creating new header cell")
//         var tableheadCell= document.createElement("th")
//         console.log("\tsetting header text to "+arr[i]);
//         tableheadCell.innerText = arr[i];
//         ret.append(tableheadCell)
//     }
//     return ret
// }

// function genTableFooter(arr) {
//     //console.log("get here")
//     //row to return
//     var ret=document.createElement("tr");  

    
//     for(var i=0; i<arr.length; i++) {
//         console.log("creating new header cell")
//         var tableheadCell= document.createElement("th")

//         if(arr[i]==null) {


//             tableheadCell.innerHTML = "&nbsp;"

//         } else {
//             if (arr[i].text!=undefined) {
//                 console.log("\tsetting header text to "+arr[i].text);
//                 tableheadCell.innerText = arr[i].text;
//             } 
//             if(arr[i].colSpan!=undefined) {
//                 tableheadCell.colSpan=arr[i].colSpan;
//             }
//         }
//         ret.append(tableheadCell)
//     }
//     return ret
// }

//render currently loaded content
//also demonstrates how to use QUIET_LOCAL vs DEBUG_UTILS correctly

function setBar(id,text,count,arraylength,fillin) {
    //setBar("pb_complete","Completed",Math.round(completedCount*100/arrayOfContent.length))
    var val=Math.round(count*100/arraylength)
    if (fillin==undefined) { 
        $("#"+id).css("width",val+"%")
    } else {
        $("#"+id).css("width",fillin+"%") 
    }
    $("#"+id).html(text+" ("+count+"/"+arraylength+") "+val+"%")
    console.log("setting id "+id+" to "+text+" "+val+"% ("+count+"/"+arraylength+")")
}

function render() {
    var readOnly = $("#roEnable").is(':checked');
    var isEmpty  = arrayOfContent.length === 0;

    $("#saveButton").prop("disabled", readOnly);
    $("#addButton").prop("disabled", readOnly || indexMode);
    $("#newListBtn").toggle(indexMode && !readOnly);

    // Empty-state visibility
    $("#hideshowButton").prop("disabled", isEmpty);
    $("#hideshowBlockedButton").prop("disabled", isEmpty);
    $(".progress-wrap").toggle(!isEmpty && !indexMode);
    $("#empty-msg").toggle(isEmpty && !indexMode);

    resetCounter()
    var blockedCount=0
    var completedCount=0
    var inprogressCount=0
    var todoCount=0
    reIndex();
    var QUIET_LOCAL=true

    var t=document.getElementById(globalEL)
    t.classList.toggle('index-view', indexMode);
    t.classList.toggle('ro-mode', readOnly);
    t.innerHTML=''

    if (!isEmpty) {
        var subjectLabel = indexMode && currentFilename
            ? currentFilename.split('/')[0].replace(/^\w/, function(c){ return c.toUpperCase(); })
            : "The Item";
        t.appendChild(genTableHeader(
            [
                "",
                "Complete",
                "Control",
                "Priority",
                subjectLabel,
                "votes",
                "Period (in days)",
                "Next Due Date",
                "Cooldown"
            ]));
    }
        
   


//    var content="<tr><th>Complete</th><th>Control</td><th>Priority</th><th>The Item</th><th>votes</th><th>Period (in days)</th><th>Next Due Date</th><th>Cooldown</th></tr>";
        var content=""
    QUIET_LOCAL || console.log("length of array="+arrayOfContent.length);
    //console.log(JSON.stringify(arrayOfContent,null,3));
    for(var i=0; i<arrayOfContent.length; i++) {

        t.appendChild(genRow(i))

        //content+=renderRow(i);
		// for recurring expiration
    	if(arrayOfContent[i].periodic!=undefined && arrayOfContent[i].periodic) {
			QUIET_LOCAL || console.log("==>"+i+"th can expire");    

			//new variables:
			//periodic - boolean; if true, this item is a recurring item
			//nextDue  - milliseconds since the EPOC / UTC, usually in the future; this is the next due date for this recurring item
			//period   - duration between due dates in days		

            if(isDueNow(arrayOfContent[i].nextDue)) {
                QUIET_LOCAL || console.log("\tdue now and again in "+arrayOfContent[i].period+" days");
            } else {
                QUIET_LOCAL || console.log("\tdue in the future: "+EpocMStoISODate(arrayOfContent[i].nextDue));
			} 
		} else {
			QUIET_LOCAL || console.log("==>"+i+"th does not expire");
		}
        if(arrayOfContent[i].onHold) blockedCount++
        else if(arrayOfContent[i].inProgress && !arrayOfContent[i].skip) inprogressCount++
        else if(arrayOfContent[i].skip) completedCount++
        else todoCount++
    }

    //content+="<tr><td name=\"delcol\">&nbsp;</td><td>&nbsp;</td><td>&nbsp;</td><td>Totals</td><td>"+TotalVotes(arrayOfContent)+"</td><td colspan=3>=====</td></tr>";
    
    vtotal=inprogressCount+todoCount





    setBar("pb_complete","Completed",completedCount,arrayOfContent.length) // Math.round(completedCount*100/arrayOfContent.length))
    setBar("pb_inprogress","In Progress",inprogressCount,arrayOfContent.length) //Math.round(inprogressCount*100/arrayOfContent.length))
    setBar("pb_blocked","Blocked",blockedCount,arrayOfContent.length)//Math.round(blockedCount*100/arrayOfContent.length))
    setBar("pb_todo","Todo",
        arrayOfContent.length-blockedCount-inprogressCount-completedCount,arrayOfContent.length,
        100-(
            Math.round((completedCount)*100/arrayOfContent.length) + 
            Math.round((inprogressCount)*100/arrayOfContent.length) +
            Math.round((blockedCount)*100/arrayOfContent.length)
            ))
    
    //Math.round((arrayOfContent.length-blockedCount-inprogressCount-completedCount)*100/arrayOfContent.length))

    // for cooldown
    var now=new Date();
    var future=new Date(60000+now.getTime());
    QUIET_LOCAL || console.log("==== "+(future.getUTCMilliseconds()-now.getUTCMilliseconds()));
    QUIET_LOCAL || console.log("now: "+formatedDate(now));
    QUIET_LOCAL || console.log("future: "+formatedDate(future));



    if(completedHidden) $(".completeClass").hide()
    if(blockedHidden)   $(".blockedClass").hide()

    updateVoteButton();
}

function OLDrender() {

    var QUIET_LOCAL=true

    //console.log("---> render()::QUIET_LOCAL="+QUIET_LOCAL)
    //console.log("---> render()::DEBUG_UTILS="+DEBUG_UTILS)






    var content="<tr><th>Complete</th><th>Control</td><th>Priority</th><th>The Item</th><th>votes</th><th>Period (in days)</th><th>Next Due Date</th><th>Cooldown</th></tr>";
    QUIET_LOCAL || console.log("length of array="+arrayOfContent.length);
    //console.log(JSON.stringify(arrayOfContent,null,3));
    for(var i=0; i<arrayOfContent.length; i++) {
        content+=renderRow(i);
		// for recurring expiration
    	if(arrayOfContent[i].periodic!=undefined && arrayOfContent[i].periodic) {
			QUIET_LOCAL || console.log("==>"+i+"th can expire");    

			//new variables:
			//periodic - boolean; if true, this item is a recurring item
			//nextDue  - milliseconds since the EPOC / UTC, usually in the future; this is the next due date for this recurring item
			//period   - duration between due dates in days		

            if(isDueNow(arrayOfContent[i].nextDue)) {
                QUIET_LOCAL || console.log("\tdue now and again in "+arrayOfContent[i].period+" days");
            } else {
                QUIET_LOCAL || console.log("\tdue in the future: "+EpocMStoISODate(arrayOfContent[i].nextDue));
			} 
		} else {
			QUIET_LOCAL || console.log("==>"+i+"th does not expire");
		}
    }

    content+="<tr><td name=\"delcol\">&nbsp;</td><td>&nbsp;</td><td>&nbsp;</td><td>Totals</td><td>"+TotalVotes(arrayOfContent)+"</td><td colspan=3>=====</td></tr>";
    
    //temporarily, don't draw the table
    document.getElementById(globalEL).innerHTML=content;
    // for cooldown
    var now=new Date();
    var future=new Date(60000+now.getTime());
    QUIET_LOCAL || console.log("==== "+(future.getUTCMilliseconds()-now.getUTCMilliseconds()));
    QUIET_LOCAL || console.log("now: "+formatedDate(now));
    QUIET_LOCAL || console.log("future: "+formatedDate(future));

}

function rebuildListSelector(s,l,desired) {
    const DEBUG_LOCAL=false
    DEBUG_LOCAL && console.log("==>rebuildListSelector("+s+",list,"+(desired || "none")+")");
    $('#'+s)
    .find('option')
    .remove().end();
    var selector = document.getElementById(s);
    var retIndex=0
    //loop through lists for this subject
    for(var i=0; i<l.length; i++) {

        var opt = document.createElement('option');
        opt.innerHTML=l[i].subject || l[i];
        opt.value=i;
        DEBUG_LOCAL && console.log("===>adding "+opt.innerHTML+" as value "+opt.value)
        DEBUG_LOCAL && console.log("comparing "+l[i]+" vs "+desired)
        if(desired!=undefined && (desired==l[i] || desired==l[i].subject)) {
            retIndex=i
        }

        selector.appendChild(opt);
        DEBUG_LOCAL && console.log("looking at "+(i+1)+" of "+l.length)
    } 
    DEBUG_LOCAL && console.log("val of "+s+": "+$('#'+s).val())
    
    $('#'+s).val(retIndex);


    //-- clone id and set it -- START
    //let p = document.getElementById(s)
    //let p_prime = p.cloneNode(true)
    //new-subject-list-selector
    //document.getElementById("new-"+s).
    

    return retIndex;
}

//move to utils.js
// $.fn.checkUtils = function() {
//     console.log("get here")
//     return this.append('<p>utils is Go!</p>');
// };


// function titleCase(str) {
//     let upper = true
//     let newStr = ""
//     for (let i = 0, l = str.length; i < l; i++) {
//         // Note that you can also check for all kinds of spaces  with
//         // str[i].match(/\s/)
//         if (str[i] == " ") {
//             upper = true
//             newStr += str[i]
//             continue
//         }
//         newStr += upper ? str[i].toUpperCase() : str[i].toLowerCase()
//         upper = false
//     }
//     return newStr
// }


// function toggle(id) {
// 	var x = document.getElementById(id+"_inner");
// 	if (x.style.display === "none") {
// 		x.style.display = "block";
// 	} else {
// 		x.style.display = "none";
// 	}
// 	var y = document.getElementById(id+"_hidden");

// 	if (y.style.display === "none") {
// 		y.style.display = "block";
// 	} else {
// 		y.style.display = "none";
// 	}
// }


// function makeID(length) {
//     var result           = '';
//     var characters       = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
//     var charactersLength = characters.length;
//     for ( var i = 0; i < length; i++ ) {
//       result += characters.charAt(Math.floor(Math.random() * 
//  charactersLength));
//    }
//    return result;
// }

// function href(url,label) {
//    if(label==undefined) return "<href=\""+url+"\">"+url+"</a>";

//    return "<href=\""+url+"\" target=_>"+label+"</a>";
// }


function dndToggled() { /* DnD is always on; disabled only in read-only mode */ }

function roToggled() {
    // console.log("roEnabled is checked? "+$("#roEnable").is(':checked'))
    // if($("#roEnable").is(':checked')) {
    //   $("#roEnable").prop('checked',false)
    // } else {
    //   $("#roEnable").prop('checked',true)
    // }
    console.log("roEnabled is checked now?  "+$("#roEnable").is(':checked'))

    //$("#roEnable").prop('checked',true)


    render();
}


function editFlip(i) {
    $("#nonediting"+i).toggle();
    $("#editing"+i).toggle();
}

function saveeditName(ele,i) {
    if(event.key === 'Enter' ) {
        //alert(ele.value+" i="+i);
        console.log("set name to "+ele.value)
        arrayOfContent[i].name=ele.value;
        $("#nonediting"+i).html(renderItem(i));
        editFlip(i);
        console.log("saving....")
        saveit();
    } else if(event.key === 'Escape') {
        ele.value=arrayOfContent[i].name;
        editFlip(i);

    }

}
function saveeditJSON(ele,i) {
    if(event.key === 'Enter' ) {
        //alert(ele.value+" i="+i);
        console.log("set json to "+ele.value)
        if(ele.value!=undefined) {
            arrayOfContent[i].json=ele.value;
        }
        $("#nonediting"+i).html(renderItem(i));
        editFlip(i);
        console.log("saving....")
        saveit();
    } else if(event.key === 'Escape') {
        if(arrayOfContent[i].json!=undefined) {
            ele.value=arrayOfContent[i].json;
        }
        editFlip(i);

    }

}

function renderItem(i,content) {
    var content="";
    var prepend="";
    var append="";
    if(arrayOfContent[i].periodic!=undefined && arrayOfContent[i].periodic && isDueNow(arrayOfContent[i].nextDue)) {
        prepend="<strong><b>";
        append="</b></strong>";
    }
    if(arrayOfContent[i].json!=undefined) {
        content+=prepend+arrayOfContent[i].name+append+" "+
        "<a href=\"javascript:SaveAndLoad('"+arrayOfContent[i].json+"')\">"+
        "<i class=\"fas fa-external-link-alt\"></i>"+
        "</a>";
    } else {
        content+=prepend+embedURL(arrayOfContent[i].name)+append;
    }
    return content;
}