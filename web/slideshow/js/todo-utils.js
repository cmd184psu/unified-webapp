

const COOLDOWN_TIME=600000
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
    //add delete column
    //var row="<td name=\"delcol\"><button onclick=\"deleteit("+i+")\">Delete</button></td>";
    var row="";
    var trophy="";

    var d=new Date();
    if(arrayOfContent[i].winner) {
        trophy="<td><i class=\"fas fa-trophy\"></i></td>";
        arrayOfContent[i].expires=d.getTime()+COOLDOWN_TIME;
        
    }

    

    var updown="<table>";
    
    //if(arrayOfContent[i].onHold) updown+="<tr bgcolor=pink>";
    //else 

    updown+='<tr>';
    //if(arrayOfContent[i].onHold) trbit="<tr bgcolor=pink>";
    //else  if(arrayOfContent[i].inProgress) trbit="<tr bgcolor=lightgreen>";
    //else trbit="<tr>";
    trbit=""
// up / down deprecated ;  gripit not needed
    //    updown+="<td><span onclick=\"gripIt("+i+")\"><i class=\"fas fas fa-grip-lines\"></i></span></td>";
//    updown+="<td><span onclick=\"moveUp("+i+")\"><i class=\"fas fa-angle-double-up\"></i></span></td>";
    if (! $("#roEnable").is(":checked")) {
      updown+="<td><span onclick=\"deleteit("+i+")\"><i class=\"fa fa-trash\"></i></td>";
    } else {
      updown+="<td></td>"
    } 
    if(arrayOfContent[i].skip) {
        updown+="<td colspan=5></td>";
    } else if ($("#roEnable").is(":checked")) {
      updown+="<td colspan=5>"+trophy+"</td>";
    } else {    
        updown+="<td>&nbsp;&nbsp;&nbsp;</td>";
        updown+="<td><span onclick=\"onHoldFlip("+i+")\"><i class=\"fas fa-hand-paper\"></i></td>";
        updown+="<td><span onclick=\"inProgressFlip("+i+")\"<i class=\"fas fa-play\"></i></td>";
        updown+="<td><span onclick=\"editFlip("+i+")\"<i class=\"fas fa-edit\"></i></td>";
        updown+="<td>"+trophy+"</td>";
    }

    updown+="</tr></table>";


   
    //add checkbox
    if(arrayOfContent[i].skip || (!arrayOfContent[i].winner && isInCoolDown(arrayOfContent[i]))) {
        row="<td><input type=\"checkbox\" checked onclick=\"dontskipit("+i+")\"/></td>";
        row+="<td>"+updown+"</td>";
        row+="<td>"+(i+1)+"</td>";
        row+="<td><strike>"+arrayOfContent[i].name+"</strike></td>";
    } else {
        row="<td><input type=\"checkbox\" onclick=\"skipit("+i+")\"/></td>";
        row+="<td>"+updown+"</td>";
        row+="<td>"+(i+1)+"</td>";


        
        row+="<td><div id=nonediting"+i+">";
        row+=renderItem(i)

        var n=arrayOfContent[i].name
        var j=arrayOfContent[i].json

        if (j==undefined || j=="undefined") {
            j=""
        }
        
        row+="</div><div id=editing"+i+" style=display:none><input size=50 text value=\""+n+"\" onkeydown=\"saveeditName(this,"+i+")\" /><input size=50 text value=\""+j+"\" onkeydown=\"saveeditJSON(this,"+i+")\" />"



        row+="</div></td>"
    }
    

    //add vote count
    if(arrayOfContent[i].votes==undefined)
        row+="<td>&nbsp;</td>";
    else
        row+="<td>"+arrayOfContent[i].votes+"</td>";


	//new variables:
	//periodic - boolean; if true, this item is a recurring item
	//nextDue  - milliseconds since the EPOC / UTC, usually in the future; this is the next due date for this recurring item
	//period   - duration between due dates in days		

    // var content="<tr><th>Complete</th><th>Control</td><th>Priority</th><th>The Item</th><th>votes</th><th>Ready?</th><th>Period (in days)</th><th>Next Due Date</th></tr>";
    if(arrayOfContent[i].skip) {
        row+="<td></td><td>"+EpocMStoISODate(arrayOfContent[i].completedOn*1000)+"</td>";
    } else if(arrayOfContent[i].periodic==undefined || !arrayOfContent[i].periodic) {
        row+="<td></td><td></td>";
    } else {
        var dueDate=new Date(arrayOfContent[i].nextDue);

        row+="<td>"+arrayOfContent[i].period+"</td>";
        row+="<td><table><tr><td width=90px>"+formatedDate(dueDate)+"&nbsp;</td><td><span onclick=\"resetDueDate("+i+")\"><i class=\"fas fa-sync\"></i></span></td></tr></table></td>";
    }

    
    //cool down
    var timeRemaining=0;
    var coolDown="Cool down";
    if(arrayOfContent[i].expires==undefined || (arrayOfContent[i].expires-d.getTime()<=0)) { 
        coolDown="Ready";
    } 
    row+="<td><table><tr><td width=60px>"+coolDown+"&nbsp;</td><td><span onclick=\"resetCoolDown("+i+")\"><i class=\"fas fa-sync\"></i></span></td></tr></table></td>";

    
    //return "<tr>"+row+"</tr>";
//    return trbit+row+"</tr>";
    return trbit+row;
}


function compare(e) {
    if(!$("#dndEnable").is(':checked')) return; 
    var p=arrayOfContent[dragging]
    arrayOfContent.splice(dragging, 1)
    //insert globalData[dragging] in draggedOver's spot
    arrayOfContent.splice(draggedOver, 0, p)
    console.log(JSON.stringify(arrayOfContent,null,3))
    //reIndex()
    
    //console.log(JSON.stringify(arrayOfContent,null,3))

    saveit()

    render()
  }
      
  function setDraggedOver(e) {
    if(!$("#dndEnable").is(':checked')) return; 
    e.preventDefault();
    draggedOver = parseInt(e.target.parentNode.id.substr(4))
  }
  
  function setDragging(e) {
      if(!$("#dndEnable").is(':checked')) return; 
    //list2 becomes 2, meaning that the 3rd row (index=2) is dragging right now
    dragging = e.target.id.substr(4)
  }





function genRow(i) {
    var node= document.createElement("tr");    
    var item= arrayOfContent[i];

    node.draggable = $("#dndEnable").is(':checked');
    if(arrayOfContent[i].skip) {
        node.className="draggableRow completeClass"
    } else if(arrayOfContent[i].onHold) {
        node.className="draggableRow blockedClass"
    } else {
        node.className="draggableRow"
    }
      
      node.id="list"+item.idx
      
      if(arrayOfContent[i].onHold) node.style.backgroundColor = "pink"
      else  if(arrayOfContent[i].inProgress) node.style.backgroundColor = "lightgreen"
      
      //TODO: this comes next!!!
      node.addEventListener('drag', setDragging) 
      node.addEventListener('dragover', setDraggedOver)
      node.addEventListener('drop', compare) 

    node.innerHTML=renderRow(i)

      //hold it
    //   var subnode=document.createElement("td")
    //   subnode.innerText=item.col1
    //   node.appendChild(subnode)
    //   if(item.col2!=undefined) {
    //     subnode=document.createElement("td")
    //     subnode.innerText=item.col2
    //     node.appendChild(subnode)
    //   }
    return node;

}
function eligibleToVote(item) {
    var d=new Date();
    if(item.skip || item.onHold) return false;
    if(item.expires!=undefined && item.expires-d.getTime()>0) return false;
    return true;
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
    render();
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
    $("#dndEnable").prop("disabled",false);
    if($("#dndEnable").is(':checked') && $("#roEnable").is(':checked')) {
      $("#dndEnable").prop('checked',false)
      dndToggled()
    }
    //if(!$("#saveButton").is(':disabled')) {
    $("#saveButton").prop("disabled",$("#roEnable").is(':checked'));
    //}
//    if(!$("#addButton").is(':disabled')) {
    $("#addButton").prop("disabled",$("#roEnable").is(':checked'));
//    }
//    if(!$("#dndEnable").is(':disabled')) {
    $("#dndEnable").prop("disabled",$("#roEnable").is(':checked'));

//    }

    resetCounter()
    var completeClassHidden=$(".completeClass").is(":hidden")
    var blockedClassHidden=$(".blockedClass").is(":hidden")
    var blockedCount=0
    var completedCount=0
    var inprogressCount=0
    var todoCount=0
    reIndex();
    var QUIET_LOCAL=true

    //console.log("---> render()::QUIET_LOCAL="+QUIET_LOCAL)
    //console.log("---> render()::DEBUG_UTILS="+DEBUG_UTILS)

    var t=document.getElementById(globalEL)
    t.innerHTML=''
    t.appendChild(genTableHeader(
        [
            "Complete",
            "Control",
            "Priority",
            "The Item",
            "votes",
            "Period (in days)",
            "Next Due Date",
            "Cooldown"
        ]));
        
   


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

    t.appendChild(genTableFooter([ null, null,{ "text" : vtotal }, { "text" : "Totals" }, { "text" : TotalVotes(arrayOfContent) }, { "text" : "=====", "colSpan" : 3 }]))




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



    if(completeClassHidden) $(".completeClass").hide()
    if(blockedClassHidden) $(".blockedClass").hide()
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


function dndToggled() {
    var rows = document.querySelectorAll(".draggableRow");
    for(i=0; i<rows.length;i++) {
        //console.log("disable draggable on row "+i+" "+rows[i].innerHTML)
        rows[i].draggable=$("#dndEnable").is(':checked');
    }
}

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