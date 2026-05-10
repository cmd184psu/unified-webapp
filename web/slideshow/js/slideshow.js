var slideIndex = 0;
var element_to_hide= undefined
var paused=true
var jpgs=[]
var counter=0;
let cancelGoForwards=false;
let scrollLock=false
let goforwardlock=false

const subject_selector = "subject-selector"

function refreshDots() {
	var dots = document.getElementsByClassName("dot");
	for (var i = 0; i < dots.length; i++) {
		dots[i].className = dots[i].className.replace(" active", "");
	}
	dots[(slideIndex) % dots.length].className += " active";
}  

function getDelay(init) {
	if(!isNaN($("#delay").val()) && $("#delay").val()>0) {
		return $("#delay").val()*1000;
	}
	return 5000;
}

function recurring_goForwards() {
	if(paused) return;
	goForwards();
	if(!paused) setTimeout(recurring_goForwards, getDelay()); // Change image every 5 seconds

}
function pauseShow() {
	console.log("PAUSED!");
	document.getElementById("playBtn").style.display="block";
	document.getElementById("pauseBtn").style.display="none";
	paused=true;
}

function playShow() {
	console.log("PLAY!");
	document.getElementById("playBtn").style.display="none";
	document.getElementById("pauseBtn").style.display="block";
	//show current slide
	//slideIndex--;
	paused=false;
	recurring_goForwards();
}

function goBackwards(pausestate) {
	if(pausestate!=undefined) paused=pausestate;
	showImage(slideIndex-1);
}

function goForwards(pausestate) {
	if(goforwardlock) {
		return
	}
	goforwardslock=true
	if(scrollLock) {
		console.log("goForwards blocked by scrollLock")
		goforwardslock=false
		return
	}
	if(pausestate!=undefined) paused=pausestate;
	if(cancelGoForwards) {
		cancelGoForwards=false
		goforwardslock=false
		return
	}
	showImage(slideIndex+1);
	goforwardslock=false
}

function showImage(newslideIndex) {
	//hide current element
	var s=0
	if(element_to_hide==undefined) {
		console.log("nothing to hide yet")
	} else {
		console.log("setting element to hide ( "+element_to_hide+") to none");
	}

	slideIndex=newslideIndex;
	if(slideIndex>jpgs[$('#'+subject_selector).val()].entries.length-1) {
		s=Number($('#'+subject_selector).val())+1
		if(s>jpgs.length-1) s=0;
		console.log("====== new subject: "+s)
		slideIndex=0;
		$('#'+subject_selector).val(Number(s))
		loadCurrentSubject();
		saveSubject()
	}

	if(slideIndex<0) {
		console.log("\t--->CAUGHT: index < 0");
		s=Number($('#'+subject_selector).val())-1;

		if(s<0) {
			s=jpgs.length-1;
		}	
		$('#'+subject_selector).val(Number(s))
		loadCurrentSubject();
		console.log("\t--->revert subject: "+$('#'+subject_selector).val());
		console.log("\t---->set slideIndex to length -1")
		slideIndex=jpgs[$('#'+subject_selector).val()].entries.length-1;
	}
	
	//show next element
	var element_to_show="subject_slide"+slideIndex;
	console.log("setting element to show ( "+element_to_show+") to block");
	console.log("\tsetting element to show ( subject_slide"+slideIndex+") to block");

	if(element_to_hide!=undefined) {
		console.log("want to hide:")
		console.log("\t"+element_to_hide)	
		if(document.getElementById(element_to_hide)!=null && document.getElementById(element_to_hide).style!=null) {
			document.getElementById(element_to_hide).style.display="none";
		}
	}
	try {
		if(element_to_show!=undefined) {
			document.getElementById(element_to_show).style.display="block";
		} else {
			console.log("element to show should not be undefined")
			console.log("it should be: subject_slide"+slideIndex)
			throw error
		}
	} catch (err) {
		console.error("error:"+error)
	}
	element_to_hide=element_to_show
	refreshDots();
}

function nextSubject() {
	pauseShow();
	if(element_to_hide!=undefined) document.getElementById(element_to_hide).style.display="none";
	var s=Number($('#'+subject_selector).val())+1
	slideIndex=0;
	if(s>jpgs.length-1) s=0;
	$( '#'+subject_selector ).val(Number(s));
	loadCurrentSubject();

	showImage(0)
	saveSubject()
}

function prevSubject() {
	pauseShow();
	if(element_to_hide!=undefined) document.getElementById(element_to_hide).style.display="none";
	var s=Number($('#'+subject_selector).val())-1;
	slideIndex=0;
	if(s<0) s=jpgs.length-1;
	$( '#'+subject_selector ).val(Number(s));
	loadCurrentSubject();
	showImage(0)
	saveSubject()
}

function changeSubject() {
	pauseShow();
	if(element_to_hide!=undefined) document.getElementById(element_to_hide).style.display="none";
	var s=Number($( '#'+subject_selector).val());
	slideIndex=0;
	if(s>jpgs.length-1) s=0;
	$( '#'+subject_selector ).val(Number(s));
	loadCurrentSubject();
	showImage(0)
	saveSubject()
}

function fixRes() {
// <td>Max Width: <div id="mw">2048</div></td>
//   		<td><div class="slidecontainer"><input type="range" min="448" max="2048" value="2048" id="maxWidth" onchange="fixRes()"></div>px</td>
//   		<td>Max Height: <div id="mh">800</div></td>
//   		<td><div class="slidecontainer"><input type="range" min="0" max="5" value="5" id="maxHeight" 
	$("#mw").html($("#maxWidth").val()*200+1000);
	$("#mh").html($("#maxHeight").val()*100+300);
	$('.dimcontrol').css('max-width',$("#mw").html()+'px');
	$('.dimcontrol').css('max-height',$("#mh").html()+'px');
	console.log("fixRes: "+$("#mw").html()+" x "+$("#mh").html());
}

function sendMsg(m) {
	$("#msgbar").html(m)
	$("#msgbar").show()
	
	setTimeout(function() { $("#msgbar").html("")
		  $("#msgbar").hide() }   ,10000)
}

function saveSubject() {
	console.log("Save button clicked")
	config.defaultSubject=jpgs[$('#'+subject_selector).val()].subject
	config.defaultItem=undefined

	$.ajax({
        url: 'config',  //relative target route
        type: 'post',  //method
        dataType: 'json',  //type to receive... json
        contentType: 'application/json', //contenttype to send
        success: function (data) {
           $('#saveButton').prop('disabled', false);
           console.log("success in saving config")
           sendMsg(data.msg)
       },
       data: JSON.stringify(config), // content to send; has to be stringified, even though it's application/json
       error: function(err){   //something bad happened and ajax is unhappy
            console.log(JSON.stringify(err,null,3));
            alert(err.responseJSON.error);
       }

   }).done(function(data) {
       console.log("done");
       //re-enable save button
       $('#saveButton').prop('disabled', false);
       
   });
}

function deleteSubject() {
	if(config.deleted==undefined) config.deleted=[]
	config.deleted.push(jpgs[$('#'+subject_selector).val()].subject)
}

//Load the ith subject into container c
function loadSubject(i,c) {
	var restartshow=false
	if(!paused) {
		restartshow=true
		console.log("pausing show during load subject")
		pauseShow()
	}
 

	console.log("loading subject i="+i+" which is ")
	console.log(jpgs[i].subject+" into c=#"+c)

	$("#"+c).hide();
	$( "#"+c ).empty();
	const images=[];
	for(var j=0; j<jpgs[i].entries.length; j++) {
		var content_to_add_to_dom="<div class=\"mySlides fade\" id=\"subject_slide"+j+"\" style=\"display:none\">"+
			"<div class=\"numbertext\">"+(j+1)+" / "+(jpgs[i].entries.length)+" "+jpgs[i].entries[j]+"</div>" +
			 "<img src=\""+config.prefix+"/"+jpgs[i].entries[j]+"\" class=\"dimcontrol\" onclick=\"onImageClick()\" id=\"subject_slide"+j+"_img\">" +
			 "</div>";
		 //console.log("adding to dom: "+content_to_add_to_dom);      
		$( "#"+c ).append( content_to_add_to_dom );
		images.push(config.prefix+"/"+jpgs[i].entries[j])
			//slidecount++;
	}

//	console.log ( "images were: " + $("#kbc").attr("images") )
//	$("#kbc").attr("images","");
//	console.log ( "images now (1): " + $("#kbc").attr("images") )
	$("#kbc").attr("images", images.join(' '))
//	console.log ( "images now (2): " + $("#kbc").attr("images") )
	$("#"+c).show();
	fixRes();
	
	if(restartshow) {
		console.log("restarting the show in 5 seconds")
		setTimeout(function() { playShowAlt() },5000)
	}
}

function loadCurrentSubject() { loadSubject($('#'+subject_selector).val(),'slideshow-container') }



async function startSlideShow() {
    //load /config into memory
	config=await ajaxGet("config/");

    jpgs=await ajaxGet("/items");

	console.log("found "+jpgs.length+" subjects.")

	var slidecount=0;
	slideIndex=0;

	var select = document.getElementById(subject_selector);
	var i=0;
	for( ; i<jpgs.length; i++) {   
		var opt = document.createElement('option');
    	opt.value = i;
    	opt.innerHTML = jpgs[i].subject;
    	select.appendChild(opt);
		console.log("item (subject):"+jpgs[i].subject)
		
	}
    
	$('#'+subject_selector).val(0)
	
	if(config.defaultSubject!=undefined) {
		console.log("looking for default subject....")
		for(var i=0; i<jpgs.length; i++) {
			if(config.defaultSubject==jpgs[i].subject) {
				
				console.log("!!!! setting selector to "+i+" !!!!")
				$('#'+subject_selector).val(i)
				
			} else {
				console.log("rejecting "+jpgs[i].subject)
			}
		}
	}

	loadCurrentSubject();
	//loadSubject($('#'+subject_selector).val(),'slideshow-container')


	console.log("show first image, selector="+$('#'+subject_selector).val())
	console.log("\tdefault subject is "+config.defaultSubject)
	showImage(0)
}
let isAtBottom = false;

function scrollBottomThenTop(duration = 2000) {
	if (paused) return
	if (scrollLock) {
		console.log("timing error, someone tried to enter function too soon")
		return
	}
	scrollLock=true
    const scrollHeight = document.documentElement.scrollHeight;
    const clientHeight = document.documentElement.clientHeight;

    function scrollTo(start, end, callback) {
		if (paused) {
			return
		}
        let startTime = null;
        const distance = end - start;

        function animation(currentTime) {
			if(paused) {
				return
			}
            if (startTime === null) startTime = currentTime;
            const timeElapsed = currentTime - startTime;
            const run = ease(timeElapsed, start, distance, duration);
            window.scrollTo(0, run);
            if (timeElapsed < duration) {
                requestAnimationFrame(animation);
            } else {
				if(paused) {
					return
				}
                if (callback) setTimeout(callback, 500); // Wait half a second before scrolling back
            }
        }

        function ease(t, b, c, d) {
            t /= d / 2;
            if (t < 1) return c / 2 * t * t + b;
            t--;
            return -c / 2 * (t * (t - 2) - 1) + b;
        }
		if(paused) {
			return
		}
        requestAnimationFrame(animation);
    }

    // First, scroll to bottom
    scrollTo(window.pageYOffset, scrollHeight - clientHeight, function() {
		if(paused) return
        // Then, scroll back to top
        scrollTo(scrollHeight - clientHeight, 0);
    });
	scrollLock=false
}


function recurring_SelfScroll() {
	if(paused) return;
	//prep for scroll
	console.log("prep for scroll")

	el="subject_slide"+slideIndex+"_img"
	console.log("subject_selector:="+el)
	const img = document.getElementById(el);
	img.style.width = '100%';
	img.style.height = '100%';
	img.style.maxHeight='4000px'
	img.style.maxWidth='4000px'
	img.style.top="100px"
	isAtBottom = false;
	//do the scroll down
	console.log("scrolling down at delay: "+getDelay()+" and then back to the top")
	scrollBottomThenTop(getDelay())

	//go forwards
	if(!paused) {
		setTimeout(
			function() {
				console.log("go forwards")
				if(paused) {
					return
				}
				goForwards();
				console.log("plant next round as timeout with delay: "+getDelay())

				if(paused) {
					return
				}

				recurring_SelfScroll()}
				, getDelay()*2+1000); // Change image every 5 seconds
		}
}

function playShowAlt() {
	console.log("PLAY!");
	document.getElementById("playBtn").style.display="none";
	document.getElementById("pauseBtn").style.display="block";
	//show current slide
	//slideIndex--;
	paused=false;
	//recurring_goForwards();
	recurring_SelfScroll();
}

function onImageClick() {
	if(paused) {
		goForwards()
	} else {
		pauseShow()
		el="subject_slide"+slideIndex+"_img"
		console.log("subject_selector:="+el)
		const img = document.getElementById(el);
		//fixRes()
		img.style.width = '';
		img.style.height = '';
		img.style.maxHeight=''
		img.style.maxWidth=''
		img.style.top="100px"

		fixRes()
		cancelGoForwards=true
//		max-width: 2000px; max-height: 800px;
		isAtBottom = false;
	}
}