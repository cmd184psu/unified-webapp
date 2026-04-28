var dataList=[]
var currentExercise=-1

function ReturnClick() {
    HideAll()
    $('#restButton').hide()
    $('#returnButton').hide()
    $('#thetable').show()
}

function HideAll() {
    $('#thetable').hide()
    for(var i=0; i<dataList.length; i++) { 
        $('#div'+i).hide()
    }    
}

function RestAck() {
  $('#restButton').hide()    
  setTimeout(UpdateTimer,1000,dataList[currentExercise].secondsPerSet)
}

function UpdateTimer() {
    console.log("updateTimer "+$('#counter').val() )
    sps=dataList[currentExercise].secondsPerSet;
    var counter=$('#counter').html()
    counter--;
    $('#counter').html(counter)

    console.log("counter="+counter+" secondsPerSet="+sps+" counter%secondsPerSet="+(counter%sps==0))

    if (counter%sps==0) {
        $('#restButton').html('Rest')
        $('#restButton').show()
    } else {    
        setTimeout(UpdateTimer,1000)
    }
}

function Exercise(i) {
  console.log("Exercise("+i+")")
  currentExercise=i
  $('#counter').html(  dataList[i].sets * dataList[i].secondsPerSet)
  HideAll()
  $('#div'+i).show()
  //setTimeout(UpdateTimer,1000)
  $('#restButton').html('Ready?')
  $('#restButton').show()
  $('#returnButton').show()

}

function RenderCell(j,obj) {
   return "<td width=50% onclick=Exercise("+j+")><img src=\"data/"+obj.image+"\"/></td>"
}

function render() {
    console.log("function render()")
    for(var i=0; i<dataList.length; i+=2) {
        console.log("dataList["+i+"]="+dataList[i].title+", dataList["+(i+1)+"]="+dataList[i+1].title)
        row="<tr>"+RenderCell(i,dataList[i])+RenderCell(i+1,dataList[i+1])+"</tr>"
        $('#thetable > tbody:last-child').append(row)
    }
    var content=""
    for(var i=0; i<dataList.length; i++) {
        console.log("dataList["+i+"]="+dataList[i].title)
        content+='<div id=div'+i+' style=display:none><strong>'+dataList[i].title+'</strong><br>'
        content+='<img src=\"data/'+dataList[i].image+'\"/><br>'
        content+='Sets: '+dataList[i].sets+'<br>'
        content+='Seconds Per Set: '+dataList[i].secondsPerSet+'<br>'
        content+='Per Day: '+dataList[i].perDay+'<br>'
        content+='</div>'
    }
    $('#therest').html(content)

}

async function startExercise() {
    //resetCounter()
    //reduceCountDown()
    //load /config into memory
	config=await ajaxGetJSON("config/");

	//load items into memory
	dataList=await ajaxGetJSON("data/data.json");

   

    
	//render it
	render();
    //$(".completeClass").hide()
}
