var config=new Object;
var arrayOfContent=[];
var topMenus=[]

const item_list_selector='item-list-selector'
const subject_list_selector='subject-list-selector'
var currentFilename=undefined
var previousFilename=undefined

const DEBUG=false

const skipsave=false
const restrictedsave=false
const showsavealert=false
//const BASE='lists/'

var SHOWALLPAGES=false;

function SaveList(content,filename) {
    if(filename==undefined) {
        throw error
        //return
    }
    DEBUG && console.log("SaveList(...content...,"+filename+");");
    $('#saveButton').prop('disabled', true);

    if(restrictedsave) {
        console.log("-- RESTRICTED SAVE MODE, for safety! --")    
        if(!filename.includes("test")) {
            $('#saveButton').prop('disabled', false);
            return
        }
    }

    if(skipsave) {
        console.log("-- SKIPPING SAVE, for safety! --")    
        $('#saveButton').prop('disabled', false);
        return
    }
    
    dropVars(); //potential bug; this function works on arrayOfContent global rather than content local
    DEBUG && console.log(JSON.stringify(content,null,3));
    
    DEBUG && console.log("OVERWRITING your data in "+filename);

    $.ajax({
        url: 'items/'+filename,  //relative target route
        type: 'post',  //method
        dataType: 'json',  //type to receive... json
        contentType: 'application/json', //contenttype to send
        success: function (data) {
           $('#saveButton').prop('disabled', false);
           console.log("success in saving content for filename: "+this.url)
           if(showsavealert) alert(data.msg)
       },
       data: JSON.stringify(content), // content to send; has to be stringified, even though it's application/json
       error: function(err){   //something bad happened and ajax is unhappy
            console.log(JSON.stringify(err,null,3));
            if(showsavealert) alert(err.responseJSON.error);
       }

   }).done(function(data) {
       console.log("done");
       //re-enable save button
       $('#saveButton').prop('disabled', false);
       
   });
}

function saveit() {
    if(currentFilename!=undefined) SaveList(arrayOfContent,currentFilename);
}


async function SelectNewFile(nf) {
    if(nf==undefined) {
        throw error
    }    

    console.log("====SelectNewFile("+nf+")")
    var p=currentFilename;
    console.log("\tcurrent file is "+currentFilename)
    console.log("\tselected file is "+lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()])
    var newsubject=nf.includes('/') && nf.split('/')[0] || lists[$('#'+subject_list_selector).val()].subject
    console.log("\twith new subject: "+newsubject)
    if(!nf.includes('/')) nf=newsubject+'/'+nf
    console.log("\tdesired file is "+nf)

    if(nf!=undefined) {
        //if current subject and desired subject (within new filename nf, then select it)
        if(lists[$('#'+subject_list_selector).val()].subject!=newsubject) {
            //console.log("need new subject: "+newsubject)
            for(var i=0; i<lists.length; i++) {
                if(lists[i].subject==newsubject) {
                    $('#'+subject_list_selector).val(i)
                    //this.subjectListIndex=i
                    console.log("found and selected new subject ("+newsubject+")")
                    rebuildListSelector(subject_list_selector,lists,newsubject)
                    continue;
                }
            }
        } else {
            console.log("sticking with same subject ("+newsubject+")")
        }
    }

    //either the subject didn't change or it did; our lists should be correct now
    console.log("current subject index = "+$('#'+subject_list_selector).val());
    console.log("current list index = "+rebuildListSelector(item_list_selector,lists[$('#'+subject_list_selector).val()].entries,nf))
   
    if(currentFilename!=undefined && config.autosave) SaveList(arrayOfContent,currentFilename);

    arrayOfContent=await ajaxGetJSON('items/'+lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()]);
    currentFilename=lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()];
    render();

    $("#backBTN").prop("disabled",p==currentFilename);
    if(p==currentFilename) previousFilename=undefined;
    else previousFilename=p;
}

function SaveAndLoad(newfilename) {
    SelectNewFile(newfilename);
}

function changeItem() {
    SelectNewFile(lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()]);
}

function changeSubject() {
    SelectNewSubject(lists[$('#'+subject_list_selector).val()].subject,lists[$('#'+subject_list_selector).val()].subject+"/index."+config.ext)
}

function SelectNewSubject(newsubject,newfile) {
    if(newsubject==undefined && newfile==undefined) {
        console.log("neither subject nor file is defined; checking defaults")
        newfile=config.defaultItem || (config.defaultSubject+"/index."+config.ext);
        newsubject=tj.split('/')[0]
    }
    
    console.log("want to change subject to "+newsubject+" and load list "+newfile)
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

    rebuildListSelector(item_list_selector,lists[$('#'+subject_list_selector).val()].entries,newfile)
    //newFilename=lists[$('#subject-list-selector').val()].entries[$('#list-selector').val()]
    //changeItem();
    SelectNewFile(lists[$('#'+subject_list_selector).val()].entries[$('#'+item_list_selector).val()]);
}

function revertList() {
    if(previousFilename!=undefined) {
        console.log("calling SelectNewFile("+previousFilename+")")
        SelectNewFile(previousFilename);
    }
    $("#backBTN").prop("disabled",true);
}

function addIt() {
    console.log("add new item");
    var object={};

    object.name=$("#itemName").val();
    object.votes=0;
    object.skip=false;
    if($("#itemJSON").val()!="") object.json=$("#itemJSON").val();
	if($("#itemPeriod").val()!="") {
		object.periodic=true;
		object.period=Number($("#itemPeriod").val());
		var now=new Date();
		var dtom=DaysToMS(object.period)
		console.log("days="+object.period);
		console.log("ms="+dtom);
		console.log("ms(now)="+now.getTime());
		var dueDate=new Date(now.getTime()+dtom);
		object.nextDue=dueDate.getTime();
		console.log("next due is "+object.nextDue);
		console.log("\tas date: "+EpocMStoISODate(object.nextDue));
	}
    arrayOfContent.push(object);

    $("#itemName").val("");
    $("#itemJSON").val("");
    $("#itemPeriod").val("");
    render();
    saveit();
}

function addMajorMenuDONTUSE(el,menuJSON) {
   var content='<li class="nav-item dropdown">'
   content+='<a class="nav-link dropdown-toggle" href="#" id="navbardrop2" data-toggle="dropdown">'
   content+='Section 422'
   content+='</a>'
   content+='<div class="dropdown-menu">'
   content+='<a class="dropdown-item" href="#section41">SLink 1</a>'
   content+='<a class="dropdown-item" href="#section42">SLink 2</a>'
   content+='</div>'
   content+='</li>'
   $(el).append(content);
}

async function startMenuserverDONTUSE() {
    //load /config into memory
	config=await ajaxGetJSON("config/");

	//load items into memory
	menus=await ajaxGetJSON("data.json");

    console.log(JSON.stringify(menus,null,3))

    //for(var i=0; i<menus.length; i++) {
        var i=0;
       addMajorMenu("#ul_nav_list",menus[i])
    //}
}

//move to utils
function dropDownButton(title) {
    return "<button class=\"dropbtn\">"+title+"<i class=\"fa fa-caret-down\"></i></button>";
}

function arrayToDropDown(arrayContent) {
    var content="<div class=\"dropdown-content\">";
    for(var i=0; i<arrayContent.length; i++) {
        console.log("arrayToDropDown::id="+arrayContent[i].id);

        if(arrayContent[i].url!=undefined) {
            var title=arrayContent[i].url;
            if(arrayContent[i].title!=undefined) title=arrayContent[i].title

            content+="<a href=\""+arrayContent[i].url+"\"  target=\"_blank\">"+title+"</a>"
        } else {
            content+="<a href=\"javascript:showPage(\'"+arrayContent[i].id+"\')\">"+titleCase(arrayContent[i].title)+"</a>";
        }
    }
    content+="</div>";
    return content;
}

function addMajorMenu(el,title,submenus) {
    var content="<div class=\"dropdown\">"+dropDownButton(title)+arrayToDropDown(submenus)+"</div>"
    console.log(content)
    $('#'+el).append("<div class=\"dropdown\">"+dropDownButton(title)+arrayToDropDown(submenus)+"</div>")
}

function addMajorMenuHamburger(el) {
    $('#'+el).append("<a href=\"javascript:void(0);\" style=\"font-size:15px;\" class=\"icon\" onclick=\"openMenu()\">&#9776;</a>")
}


function renderSiteRow(site, i) {
   var link="";
   if(site.url!=undefined) link=site.url;
   if(site.port!=undefined) link=site.url+":"+site.port
   if(site.prefix==undefined) site.prefix=makeID(5)+"_"
   var content="";
   if(site.label!=undefined) label=site.label;
   else label=link;

   if(link=="" && label!="") content+="<tr><td colspan=2>"+label+"</td></tr>";
   else if(link!="" && label!="" && link!=label) content+="<tr><td colspan=2><a href=\""+link+"\" target=\"_blank\">"+label+"</a> : "+link+"</td></tr>";
   else if(link!="" && label!="" && link==label) content+="<tr><td colspan=2><a href=\""+link+"\" target=\"_blank\">"+label+"</a></td></tr>";
   
   
   if(site.username!=undefined && site.username!="") {
        content+=
        "<tr><td>Username: </td><td>"+site.username+"</td></tr>"+
        "<tr><td>Password: </td><td><div id=\""+site.prefix+"pwd"+i+"_inner\" style=\"display:none\">"+  
        "<input type=text id=\""+site.prefix+"txt"+i+"\" value=\""+
        site.password+"\"></div><div id=\""+site.prefix+"pwd"+i+"_hidden\" >xxxxxxxxxx</div></td></tr>"+
        "<tr><td></td><td>"+
        "<table><tr>"+
        "<td><button type=\"button\" onclick=\"toggle(\'"+site.prefix+"pwd"+i+"\')\">Hide/Show</button></td>"+
        "<td><button type=\"button\" onclick=\"copyToClipBoard(\'"+site.password+"\')\">Copy</button></td>"+
        "</table>"+
        "</td></tr>"

    }
    return content;
}

function renderSiteTable(siteArray) {
    if(siteArray==undefined) return "";
    var content="";
    for(var i=0; i<siteArray.length; i++) {
        content+=renderSiteRow(siteArray[i],i)
    }
    return content;
}

function addPage(el,json) {
    var s=""
    if(!SHOWALLPAGES) {
        s="style=\"display:none\"";
    }
    
    var content="<div id=\""+json.id+"\" class=\"pageClass\" "+s+" ><h2>"+json.title+"</h2>"; 
    
    //if(json.url!=undefined) content+="<a href=\""+json.url+"\"  target=\"_blank\">"+json.url+"</a>";
        
    content+="<BR>";
    content+="<table style=\"border-collapse: separate; border-spacing: 15px 20px;\">"
    if(json.sites!=undefined) content+=renderSiteTable(json.sites);

    if(json.notes!=undefined) {
        content+="<tr>"+
        "<tr><td>Notes: </td>"+
        "<td><p>"+json.notes+"</p></td>"+
        "</tr>";
    }
    if(json.gdoc!=undefined) {

        content+="<tr>"+
        "<tr><td>G-Doc: </td>"+
        "<td><p>"+json.notes+"</p></td>"+
        "</tr>";
    }

    content+="</table>"+
             "</div>"
    if(SHOWALLPAGES) {
        content+="<div><a href=\"#top\"><i class=\"fas fa-arrow-up\"></i></a></div><div class=\"pageClass\"><HR/><BR></div>"

    }

    $('#'+el).append(content);
}

function showPage(el) {
    if(SHOWALLPAGES) {

        window.location = "#"+el;
    } else {    
        $(".pageClass").hide();
        console.log("show element: #"+el)
        $("#"+el).show();
    }
}

async function startMenuserver() {
    //load /config into memory
	config=await ajaxGetJSON("config/");


    SHOWALLPAGES=config.showAllPages;
	//load items into memory
	topMenus=await ajaxGetJSON("items");

	//render selectors
    //DEBUG && console.log("config.defaultSubject="+config.defaultSubject)
    //rebuildListSelector(subject_list_selector,lists,config.defaultSubject)
    
    //DEBUG && console.log("config.defaultItem="+config.defaultItem)
	//rebuildListSelector(item_list_selector,lists[$('#'+subject_list_selector).val()].entries,config.defaultItem)
    var haveSplash=false;
    //build out the menu
    const renderedPageSet=new Set();
    for(var i=0; i<topMenus.length; i++) {
        //console.log(JSON.stringify(topMenus[i],null,3));

        console.log("subject==="+topMenus[i].subject)
        topMenus[i].subject=titleCase(topMenus[i].subject)
        topMenus[i].submenus=[];
        if(topMenus[i].entries!=undefined) {
            for(var j=0; j<topMenus[i].entries.length; j++) {
               console.log("\tj="+j+" load and add as submenu: "+JSON.stringify(topMenus[i].entries[j],null,3));
               //content=await ajaxGetJSON(topMenus[i].entries[j])
               submenu_index=topMenus[i].submenus.length;
               topMenus[i].submenus.push(await ajaxGetJSON("menus/"+topMenus[i].entries[j]))
               topMenus[i].submenus[submenu_index].title=titleCase(topMenus[i].submenus[submenu_index].title)
               console.log("\tj="+j+" content="+JSON.stringify(topMenus[i].submenus[submenu_index]))
                if(!renderedPageSet.has(topMenus[i].submenus[submenu_index].id)) {
                   addPage("lowerSection",topMenus[i].submenus[submenu_index])
                   renderedPageSet.add(topMenus[i].submenus[submenu_index].id)
                }
            }
            if(topMenus[i].subject!="Splash") {
                addMajorMenu("myTopnav",topMenus[i].subject, topMenus[i].submenus)
            } else {
                haveSplash=true;
            }
        } else {
            console.log("---- entries is null for j="+j+"---");
        }
        console.log("--- NEXT ---")
    }
    console.log("entire menu heiarchy: ")
    console.log(JSON.stringify(topMenus,null,3))

    //load default topic and json
    addMajorMenuHamburger("myTopnav")

    //render splash

    if(haveSplash) {
        console.log("show splash")
        showPage('splash');
    }
	//render();
}
