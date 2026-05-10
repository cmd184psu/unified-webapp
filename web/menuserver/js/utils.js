
// NEW and improved utils.js -- used in todo, slideshow, menuserver and other projects.

const DEBUG_UTILS=false

function ajaxGet(uri) {
	return new Promise((resolve, reject) => {
        $.get(uri,"", function(result) { resolve(result); });
    });
}

function ajaxGetJSON(uri) {
    console.log("ajaxGetJSON("+uri+")")
    return new Promise((resolve, reject) => {
        fetch(uri).then(async (response)=> {
            resolve(await response.json())
        })
    })
}    

function copyToClipBoard(text){
    var c=document.getElementById('copytext');
    c.value=text;
    
    var x=document.getElementById('hiddentext');
    x.style.display="block";
    
    c.select();
    try {
        var successful = document.execCommand('copy')
        var msg = successful ? 'successfully' : 'unsuccessfully'
        alert('Copied!');
    } catch(err) {
        alert('Falied to copy.');
    }
    x.style.display="none";
}
  

function DaysToMS(days) {
	//return days*24*60*60*1000;
	return days*86400000;
}

function EpocMStoISODate(ms) {
	var d=new Date(ms);
	return formatedDate(d);
}

function isDueNow(ms) {
    var now=new Date();
    return ms<now.getTime();
}

function formatedDate(d) {
    return (d.getMonth()+1)+"/"+d.getDate()+"/"+d.getFullYear();
}

//Move to todo.js
function embedURL(str) {
    const myArr = str.split(" ");
    var newArray = [];
    for(var i=0; i<myArr.length; i++) {
        if(myArr[i].includes(";http")) {
            newArray.push("<a href=\""+myArr[i].split(';')[1]+"\" target=_blank>"+myArr[i].split(';')[0]+"</a>")
        } else {
            newArray.push(myArr[i])
        }
    }
    return newArray.join(" ")
}

function genTableHeader(arr) {
    //console.log("get here")
    //row to return
    var ret=document.createElement("tr");  
    for(var i=0; i<arr.length; i++) {
        var tableheadCell= document.createElement("th")
        tableheadCell.innerText = arr[i];
        ret.append(tableheadCell)
    }
    return ret
}

function genTableFooter(arr) {
    var ret=document.createElement("tr");  
    for(var i=0; i<arr.length; i++) {
        console.log("creating new header cell")
        var tableheadCell= document.createElement("th")
        if(arr[i]==null) {
            tableheadCell.innerHTML = "&nbsp;"
        } else {
            if (arr[i].text!=undefined) {
                console.log("\tsetting header text to "+arr[i].text);
                tableheadCell.innerText = arr[i].text;
            } 
            if(arr[i].colSpan!=undefined) {
                tableheadCell.colSpan=arr[i].colSpan;
            }
        }
        ret.append(tableheadCell)
    }
    return ret
}



function titleCase(str) {
    let upper = true
    let newStr = ""
    for (let i = 0, l = str.length; i < l; i++) {
        // Note that you can also check for all kinds of spaces  with
        // str[i].match(/\s/)
        if (str[i] == " ") {
            upper = true
            newStr += str[i]
            continue
        }
        newStr += upper ? str[i].toUpperCase() : str[i].toLowerCase()
        upper = false
    }
    return newStr
}

function toggle(id) {
	var x = document.getElementById(id+"_inner");
	if (x.style.display === "none") {
		x.style.display = "block";
	} else {
		x.style.display = "none";
	}
	var y = document.getElementById(id+"_hidden");

	if (y.style.display === "none") {
		y.style.display = "block";
	} else {
		y.style.display = "none";
	}
}

function makeID(length) {
    var result           = '';
    var characters       = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    var charactersLength = characters.length;
    for ( var i = 0; i < length; i++ ) {
      result += characters.charAt(Math.floor(Math.random() * 
 charactersLength));
   }
   return result;
}

function href(url,label) {
   if(label==undefined) return "<href=\""+url+"\">"+url+"</a>";

   return "<href=\""+url+"\" target=_>"+label+"</a>";
}

$.fn.checkUtils = function() {
    console.log("get here")
    return this.append('<p>utils is Go!</p>');
};
