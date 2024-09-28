@Library('jenkins.shared.library') _

pipeline {
  agent {
    label 'ubuntu_20_04_label'
  }
  tools {
    go "Go 1.20"
  }
  options {
    checkoutToSubdirectory('src/github.com/infobloxopen/db-controller')
  }
  environment {
    DIRECTORY = "src/github.com/infobloxopen/db-controller"
    GIT_VERSION = sh(script: "cd ${DIRECTORY} && git describe --always --long --tags",
                       returnStdout: true).trim()
    CHART_VERSION = "${env.GIT_VERSION}-j${env.BUILD_NUMBER}"
    TAG = "${env.GIT_VERSION}"
    GOPATH = "$WORKSPACE"
  }
  stages {
    stage("Setup") {
      steps {
        prepareBuild()
        sh 'echo "deb http://apt.postgresql.org/pub/repos/apt $(sudo lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list'
        sh 'sudo wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add -'
        sh 'sudo apt-get update -y'
      }
    }
    // stage("Run tests") {
    //   steps {
    //     withCredentials([string(credentialsId: 'GITHUB_TOKEN', variable: 'GitHub_PAT')]) {
    //         sh "echo machine github.com login $GitHub_PAT > ~/.netrc"
    //         sh "echo machine api.github.com login $GitHub_PAT >> ~/.netrc"
    //     }
    //     dir("$DIRECTORY") {
    //       sh "sudo apt-get update"
    //       sh "sudo apt-get -y install postgresql-client-14"
    //       sh "echo 'db-controller-name' > .id"
    //       sh "make test"
    //       sh "sudo apt-get -y remove postgresql-client"
    //     }
    //   }
    // }
    stage("Build db-controller image") {
      steps {
        dir("$DIRECTORY") {
          sh """
            echo cicd > .id
            REGISTRY=infoblox make docker-build
            REGISTRY=infoblox make docker-build-dbproxy
            REGISTRY=infoblox make docker-build-dsnexec
          """
        }
      }
    }
    // stage("Push db-controller image") {
    //   steps {
    //     withDockerRegistry([credentialsId: "${env.JENKINS_DOCKER_CRED_ID}", url: ""]) {
    //       dir("$DIRECTORY") {
    //         sh "REGISTRY=infoblox make docker-push"
    //         sh "REGISTRY=infoblox make docker-push-dbproxy"
    //         sh "REGISTRY=infoblox make docker-push-dsnexec"
    //       }
    //     }
    //   }
    // }
    stage('Push charts') {
      steps {
        dir ("${WORKSPACE}/${DIRECTORY}") {
          withDockerRegistry([credentialsId: "dockerhub-bloxcicd", url: ""]) {
            withAWS(region:'us-east-1', credentials:'CICD_HELM') {
              sh """
				make build-chart build-chart-crd
				make push-chart push-chart-crd
				make build-properties
				make build-properties-crd
				chmod a+xrw ${WORKSPACE}/${DIRECTORY}
              """
              archiveArtifacts artifacts: '*.tgz'
              archiveArtifacts artifacts: '*build.properties'
            }
          }
        }
      }
    }
  }
  post {
    success {
      dir("${WORKSPACE}/${DIRECTORY}") {
        // finalizeBuild is one of the Secure CICD helper methods
        finalizeBuild(
          sh(
              script: 'REGISTRY=infoblox make list-of-images',
              returnStdout: true
          ), getFileList("*.properties")
        )
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
