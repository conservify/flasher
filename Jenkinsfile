timestamps {
    node () {
        stage ('git') {
            checkout([$class: 'GitSCM', branches: [[name: '*/master']], userRemoteConfigs: [[url: 'https://github.com/Conservify/flasher.git']]])
        }

        stage ('build') {
            sh """
go get go.bug.st/serial.v1
make clean
make
cp build/linux-amd64/flasher ~/workspace/bin
"""
        }
    }
}
