var config=new Object;

async function startOnOff() {
 
    //load /config into memory
	config=await ajaxGetJSON("config/");

	//load items into memory
	state=await ajaxGetJSON("getstate");

    console.log("state is "+state.state)

    if(state.state) {
        $("#checkit").prop('checked', true);
    }
}

async function toggleBox() {
    console.log("toggle the box!")

    console.log("Current state is "+$("#checkit").is(':checked'))

    $("#checkit").toggle()
    console.log("NEW state is "+$("#checkit").is(':checked'))

    state=await ajaxGetJSON("togglestate");
    if (state.state==$("#checkit").is(':checked')) {

        console.log("states in sync")
    } else {
        console.log("states not in sync")

    }

}

