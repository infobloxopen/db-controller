@Library('jenkins.shared.library') _

pipeline {
  agent {
    label 'ubuntu_docker_label'
  }
  tools {
    go "Go 1.15"
  }
  options {
    checkoutToSubdirectory('src/github.com/infobloxopen/db-controller')
  }
  environment {
    DIRECTORY = "src/github.com/infobloxopen/db-controller"
    GIT_VERSION = sh(script: "cd ${DIRECTORY} && git describe --always --long --tags",
                       returnStdout: true).trim()
    TAG = "${env.GIT_VERSION}-j${env.BUILD_NUMBER}"
    GOPATH = "$WORKSPACE"
  }
  stages {
    stage("Setup") {
      steps {
        prepareBuild()
      }
    }
    stage("Run tests") {
      steps {
        dir("$DIRECTORY") {
          sh "make test"
        }
      }
    }
    stage("Build db-controller image") {
      steps {
        dir("$DIRECTORY") {
          sh "make docker-build"
        }
      }
    }
    stage("Push db-controller image") {
      when {
        anyOf { branch 'main'; buildingTag() }
      }
      steps {
        withDockerRegistry([credentialsId: "${env.JENKINS_DOCKER_CRED_ID}", url: ""]) {
          dir("$DIRECTORY") {
            // sh "make docker-push"
          }
        }
      }
    }
  }
  post {
    success {
      // finalizeBuild is one of the Secure CICD helper methods
      dir("$DIRECTORY") {
          finalizeBuild()
      }
    }
    cleanup {
      dir("$DIRECTORY") {
        sh "make clean || true"
      }
      cleanWs()
    }
  }
}
