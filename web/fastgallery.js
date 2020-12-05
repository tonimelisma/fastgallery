// check that the HTML page including us has set the pictures array
if (typeof pictures == 'undefined') {
    throw new Error("pictures array not defined")
}

// Hard-coded configuration
const videoExtension = "mp4"
const videoMIMEType = "video/mp4"

// global variable maintains currently shown picture number (pictures[] array)
var currentPicture

// create hover effect shadow for all box elements
const hoverOnBox = (event) => {
    event.target.classList.remove("box-shadow")
    event.target.classList.remove("border-gray")
    event.target.classList.add("box-shadow-large")
    event.target.classList.add("border-gray-dark")
}

const hoverOffBox = (event) => {
    event.target.classList.remove("border-gray-dark")
    event.target.classList.remove("box-shadow-large")
    event.target.classList.add("box-shadow")
    event.target.classList.add("border-gray")
}

const registerBoxEventHandlers = (element) => {
    element.addEventListener("mouseenter", hoverOnBox)
    element.addEventListener("mouseleave", hoverOffBox)
}

var boxes = document.getElementsByClassName("box")
for (let box of boxes) {
    registerBoxEventHandlers(box)
}

// create hover effect for modal navigation elements
// const hoverOnNav = (event) => {}

// logic to show and hide picture modal
const displayModal = (display) => {
    if (display) {
        document.getElementById("modal").hidden = false
        document.getElementById("thumbnails").hidden = true
    } else {
        document.getElementById("thumbnails").hidden = false
        document.getElementById("modal").hidden = true
        document.getElementById("modalMedia").innerHTML = ""
        window.location.hash = ""
    }
}

// TODO add swipe support https://stackoverflow.com/questions/2264072/detect-a-finger-swipe-through-javascript-on-the-iphone-and-android

// modal previous and next picture button logic
const preload = (number) => {
    var preloadLink = document.createElement("link")
    preloadLink.rel = "prefetch"
    preloadLink.href = encodeURI(pictures[number].fullsize)
    const fileExtension = preloadLink.href.split("\.").pop()
    if (fileExtension == videoExtension) {
        preloadLink.as = "video"
    } else {
        preloadLink.as = "image"
    }
    document.head.appendChild(preloadLink)
}
const prevPicture = () => {
    changePicture(getPrevPicture())
    preload(getPrevPicture())
}

const getPrevPicture = () => {
    if (!isNaN(currentPicture)) {
        if (currentPicture === 0) {
            return (pictures.length - 1)
        } else if (currentPicture < 0 || currentPicture >= pictures.length) {
            console.error("Invalid currentPicture, 0.." + pictures.length - 1 + ": " + currentPicture)
        } else {
            return (currentPicture - 1)
        }
    } else {
        console.error("Invalid currentPicture, NaN: " + currentPicture)
    }
}

const nextPicture = () => {
    changePicture(getNextPicture())
    preload(getNextPicture())
}

const getNextPicture = () => {
    if (!isNaN(currentPicture)) {
        if (currentPicture === pictures.length - 1) {
            return (0)
        } else if (currentPicture < 0 || currentPicture >= pictures.length) {
            console.error("Invalid currentPicture, 0.." + pictures.length - 1 + ": " + currentPicture)
        } else {
            return (currentPicture + 1)
        }
    } else {
        console.error("Invalid currentPicture, NaN: " + currentPicture)
    }
}

// function to change picture in modal, used by hashNavigate, and next/prevPicture
const changePicture = (number) => {
    thumbnailFilename = pictures[number].thumbnail
    window.location.hash = thumbnailFilename.substring(thumbnailFilename.indexOf("/") + 1)
    const fileExtension = pictures[number].fullsize.split("\.").pop()
    if (fileExtension == videoExtension) {
        document.getElementById("modalMedia").innerHTML = "<video controls><source src=\"" + encodeURI(pictures[number].fullsize) + "\" type=\"" + videoMIMEType + "\"></video>"
    } else {
        document.getElementById("modalMedia").innerHTML = "<img src=\"" + encodeURI(pictures[number].fullsize) + "\" alt=\"" + pictures[number].fullsize.substring(pictures[number].fullsize.indexOf("/") + 1) + "\" class=\"modalImage\">"
    }
    document.getElementById("modalDescription").innerHTML = pictures[number].fullsize.substring(pictures[number].fullsize.indexOf("/") + 1)
    document.getElementById("modalDownload").href = pictures[number].original
    currentPicture = number
}

// if URL links directly to thumbnail via hash link, open modal for that pic on page load
const hashNavigate = () => {
    if (window.location.hash) {
        const thumbnail = decodeURI(window.location.hash.substring(1))
        id = pictures.findIndex(item => item.thumbnail.substring(item.thumbnail.indexOf("/") + 1) == thumbnail)
        if (id != -1 && id >= 0 && id < pictures.length) {
            changePicture(id)
            displayModal(true)
        } else {
            console.error("Invalid thumbnail link provided after # in URI")
        }
    }
}

const checkKey = (event) => {
    if (event.keyCode == '37') {
        prevPicture()
    } else if (event.keyCode == '39') {
        nextPicture()
    }
}

document.onkeydown = checkKey
window.onload = hashNavigate